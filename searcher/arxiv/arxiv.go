// Package arxiv 封装 arXiv Atom API 的抓取与解析。
package arxiv

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// defaultEndpoint 用 HTTPS 而不是 HTTP：arXiv 的 export API 走明文 HTTP 时不稳定，
	// 官方与主流客户端（如 arxiv.py）都已统一切到 HTTPS。
	defaultEndpoint = "https://export.arxiv.org/api/query"

	// maxPerRequest 是 arXiv 单次请求的结果上限：max_results > 2000 会直接返回 400。
	maxPerRequest = 2000

	// defaultRequestDelay 是两次请求之间的间隔下限：arXiv API ToU 建议至少 3 秒
	//（与 arxiv.py 默认 delay 一致）。
	defaultRequestDelay = 3 * time.Second

	// requestTimeout 是单次 HTTP 请求的超时时间。
	requestTimeout = 60 * time.Second

	// maxAttempts 是同一请求（含 5xx / 网络错误）的最大尝试次数。
	maxAttempts = 3

	// defaultRetryBackoff 是重试的基础退避时长（第 n 次失败后退避 n 倍）。
	defaultRetryBackoff = 2 * time.Second
)

// Paper 是解析后的中间结构，最终由 ingest 映射为 domain.Item。
type Paper struct {
	ArxivID    string // arXiv 唯一标识，形如 http://arxiv.org/abs/2406.00001v1
	Title      string
	Abstract   string
	Authors    []string
	Categories []string
	Published  string // 原始 RFC3339 时间字符串
	Updated    string
}

// Client 是 arXiv API 客户端。除默认构造外，可通过 Option 覆盖端点与请求间隔，
// 便于测试时指向本地 mock 服务器。
type Client struct {
	endpoint      string
	httpClient    *http.Client
	requestDelay  time.Duration
	retryBackoff  time.Duration
	resultsPerReq int
}

// Option 用于覆盖 Client 的默认行为。
type Option func(*Client)

// WithEndpoint 覆盖 API 端点（测试用）。
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = endpoint }
}

// WithRequestDelay 覆盖两次请求之间的间隔（测试时设为 0 以避免等待）。
func WithRequestDelay(d time.Duration) Option {
	return func(c *Client) { c.requestDelay = d }
}

// WithHTTPClient 覆盖底层 HTTP 客户端。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithRetryBackoff 覆盖重试的基础退避时长（测试时设为 0 以避免等待）。
func WithRetryBackoff(d time.Duration) Option {
	return func(c *Client) { c.retryBackoff = d }
}

// NewClient 构造一个 arXiv API 客户端。
func NewClient(opts ...Option) *Client {
	c := &Client{
		endpoint:      defaultEndpoint,
		httpClient:    &http.Client{Timeout: requestTimeout},
		requestDelay:  defaultRequestDelay,
		retryBackoff:  defaultRetryBackoff,
		resultsPerReq: maxPerRequest,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchSubmitted 抓取 date 当天提交（submittedDate）的论文。
//
// maxResults <= 0 表示抓取当天全部论文：自动分页直到 API 声明的 totalResults。
// 返回结果已按 arXiv ID 去重（同一篇论文可能因分页边界重复出现）。
//
// 注意：arXiv 的 submittedDate 字段按美东时间（ET）解释，与 arXiv 官网 "new"
// 列表的日期口径一致；"某一天"指的是 ET 的当天 00:00–23:59。
func (c *Client) FetchSubmitted(ctx context.Context, date time.Time, maxResults int) ([]Paper, error) {
	day := date.Format("20060102")
	searchQuery := fmt.Sprintf("submittedDate:[%s0000 TO %s2359]", day, day)

	papers := make([]Paper, 0, 64)
	seen := make(map[string]struct{})
	start := 0
	totalResults := -1 // 以首页声明的总数为准，后续页不覆盖

	for {
		// 单次请求页大小：不超过 arXiv 上限；若指定了 -max，最后一页只取剩余量。
		page := c.resultsPerReq
		if maxResults > 0 {
			if remaining := maxResults - len(papers); remaining < page {
				page = remaining
			}
		}

		feed, err := c.fetchPage(ctx, searchQuery, start, page)
		if err != nil {
			return nil, err
		}
		if totalResults < 0 {
			totalResults = feed.TotalResults
		}

		for _, e := range feed.Entries {
			p := toPaper(e)
			if p.ArxivID == "" {
				continue // 没有唯一标识的条目无法去重，直接丢弃
			}
			if _, dup := seen[p.ArxivID]; dup {
				continue
			}
			seen[p.ArxivID] = struct{}{}
			papers = append(papers, p)
		}

		start += len(feed.Entries)
		if len(feed.Entries) == 0 {
			break // 空页说明没有更多结果，避免死循环
		}
		if totalResults >= 0 && start >= totalResults {
			break // 已取完 API 声明的全部结果
		}
		if maxResults > 0 && len(papers) >= maxResults {
			break
		}
		if err := sleep(ctx, c.requestDelay); err != nil {
			return nil, err
		}
	}
	return papers, nil
}

// fetchPage 请求一页结果。
func (c *Client) fetchPage(ctx context.Context, searchQuery string, start, page int) (*feed, error) {
	q := url.Values{}
	q.Set("search_query", searchQuery)
	q.Set("start", strconv.Itoa(start))
	q.Set("max_results", strconv.Itoa(page))
	q.Set("sortBy", "submittedDate")
	q.Set("sortOrder", "descending")

	return c.get(ctx, q)
}

// get 发起一次请求并解析 Feed；5xx/网络错误会重试，
// 若默认路径（按 DNS 直连 export.arxiv.org）全部失败则自动回退到直连 Fastly IP。
func (c *Client) get(ctx context.Context, q url.Values) (*feed, error) {
	reqURL := c.endpoint + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "arxiv-ingest/1.0")

	f, err := c.doWithClient(ctx, req, c.httpClient)
	if err == nil {
		return f, nil
	}

	fallback, fbErr := c.fetchViaFallbackIPs(ctx, req)
	if fbErr == nil {
		return fallback, nil
	}
	return nil, err // 回退也失败时返回原始错误（更贴近真实原因）
}

// doWithClient 用给定 client 请求并解析 Feed；网络错误/5xx 重试，4xx 直接返回。
func (c *Client) doWithClient(ctx context.Context, req *http.Request, hc *http.Client) (*feed, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// 退避等待：最后一次尝试失败后不再等待，直接返回错误。
		wait := func() error {
			if attempt == maxAttempts-1 {
				return nil
			}
			return sleep(ctx, c.backoff(attempt))
		}

		resp, err := hc.Do(req)
		if err != nil {
			lastErr = err
			if sleepErr := wait(); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if sleepErr := wait(); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("arxiv api 返回 %d: %s", resp.StatusCode, truncate(string(body), 500))
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, lastErr // 4xx 是请求本身的问题，重试无意义
			}
			if sleepErr := wait(); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		var f feed
		if err := xml.Unmarshal(body, &f); err != nil {
			return nil, fmt.Errorf("解析 arXiv XML 失败: %w", err)
		}
		return &f, nil
	}
	return nil, fmt.Errorf("arxiv api 请求失败（已重试 %d 次）: %w", maxAttempts, lastErr)
}

// fetchViaFallbackIPs 绕过"export.arxiv.org 解析到的 IP 被网络过滤"的问题：
// 用 arxiv.org 解析到的 Fastly IP 直连，TLS SNI 与 HTTP Host 仍保持 export.arxiv.org
// （Fastly 任播集群上同一服务承载这两个域名，已在多处网络实测可通）。
// 同时强制 HTTP/1.1：部分网络对 export.arxiv.org 的 HTTP/2 连接会静默挂起。
func (c *Client) fetchViaFallbackIPs(ctx context.Context, req *http.Request) (*feed, error) {
	ips, err := net.LookupHost("arxiv.org")
	if err != nil {
		return nil, fmt.Errorf("回退解析 arxiv.org 失败: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("回退解析 arxiv.org 没有得到任何地址")
	}

	seen := make(map[string]struct{})
	candidates := make([]string, 0, len(ips))
	for _, ip := range ips {
		if strings.Contains(ip, ":") { // 只走 IPv4
			continue
		}
		if _, dup := seen[ip]; dup {
			continue
		}
		seen[ip] = struct{}{}
		candidates = append(candidates, ip)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有可用的 arxiv.org IPv4 地址")
	}

	port := req.URL.Port()
	if port == "" {
		port = "443"
	}

	// http.Transport 可能并发建连，所以索引用原子递增，避免数据竞争。
	var idx atomic.Int64
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		ip := candidates[int(idx.Add(1)-1)%len(candidates)]
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(ip, port))
	}

	transport := &http.Transport{
		DialContext:       dial,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{}, // 禁用 HTTP/2
		ForceAttemptHTTP2: false,
	}
	// 临时 transport 用完整即释放空闲连接，避免 keep-alive 连接一直挂到进程退出。
	defer transport.CloseIdleConnections()

	return c.doWithClient(ctx, req, &http.Client{Timeout: requestTimeout, Transport: transport})
}

// ---- arXiv Atom 响应结构 ----

type feed struct {
	TotalResults int     `xml:"totalResults"` // <opensearch:totalResults>，分页结束条件
	Entries      []entry `xml:"entry"`
}

type entry struct {
	ID         string     `xml:"id"`
	Updated    string     `xml:"updated"`
	Published  string     `xml:"published"`
	Title      string     `xml:"title"`
	Summary    string     `xml:"summary"`
	Authors    []author   `xml:"author"`
	Categories []category `xml:"category"`
}

type author struct {
	Name string `xml:"name"`
}

type category struct {
	Term string `xml:"term,attr"`
}

// toPaper 把一条 Atom entry 转成 Paper。
func toPaper(e entry) Paper {
	authors := make([]string, 0, len(e.Authors))
	for _, a := range e.Authors {
		if n := strings.TrimSpace(a.Name); n != "" {
			authors = append(authors, n)
		}
	}

	cats := make([]string, 0, len(e.Categories))
	for _, c := range e.Categories {
		if t := strings.TrimSpace(c.Term); t != "" {
			cats = append(cats, t)
		}
	}

	return Paper{
		ArxivID:    strings.TrimSpace(e.ID),
		Title:      cleanText(e.Title),
		Abstract:   cleanText(e.Summary),
		Authors:    authors,
		Categories: cats,
		Published:  strings.TrimSpace(e.Published),
		Updated:    strings.TrimSpace(e.Updated),
	}
}

// cleanText 合并空白并去掉首尾空格（arXiv 标题/摘要常带换行与缩进）。
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate 截断过长的文本，仅用于错误信息。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// backoff 返回第 attempt 次失败后的等待时长（线性退避）。
func (c *Client) backoff(attempt int) time.Duration {
	return time.Duration(attempt+1) * c.retryBackoff
}

// sleep 等待 d，期间响应 ctx 取消。
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
