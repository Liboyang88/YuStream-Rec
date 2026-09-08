package main

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
	"time"
)

// 用 HTTPS 而不是 HTTP：arXiv 的 export API 走明文 HTTP 时不稳定，
// 主流客户端（如 arxiv.py）已在 2023-10 统一切到 HTTPS（见 arxiv.py PR #131）。
// 用 var 以便测试时指向本地 mock 服务器。
var arxivAPI = "https://export.arxiv.org/api/query"

// arXiv 单次请求返回结果上限：max_results > 2000 会直接返回 400。
const arxivMaxPerRequest = 2000

// arXiv API ToU 建议两次请求之间至少间隔 3 秒（与 arxiv.py 默认 delay 一致）。
var arxivRequestDelay = 3 * time.Second

// ---- arXiv Atom 响应结构 ----

type Feed struct {
	XMLName      xml.Name `xml:"feed"`
	TotalResults int      `xml:"totalResults"` // <opensearch:totalResults>，分页结束条件
	Entries      []Entry  `xml:"entry"`
}

type Entry struct {
	ID         string     `xml:"id"`
	Updated    string     `xml:"updated"`
	Published  string     `xml:"published"`
	Title      string     `xml:"title"`
	Summary    string     `xml:"summary"`
	Authors    []Author   `xml:"author"`
	Categories []Category `xml:"category"`
}

type Author struct {
	Name string `xml:"name"`
}

type Category struct {
	Term string `xml:"term,attr"`
}

// Paper 是解析后的中间结构，之后映射到 Item。
type Paper struct {
	ArxivID    string
	Title      string
	Abstract   string
	Authors    []string
	Categories []string
	Published  string
	Updated    string
}

// FetchArxivPapers 抓取 date 当天提交（submittedDate）的论文。
// maxResults <= 0 表示抓取当天全部论文（自动分页直到 totalResults）。
//
// 注意：arXiv 的 submittedDate 字段按美东时间（ET）解释，与 arXiv 官网
// "new" 列表的日期口径一致；"某一天"指的是 ET 的当天 00:00–23:59。
func FetchArxivPapers(date time.Time, maxResults int) ([]Paper, error) {
	day := date.Format("20060102")
	searchQuery := fmt.Sprintf("submittedDate:[%s0000 TO %s2359]", day, day)

	papers := make([]Paper, 0, 64)
	seen := make(map[string]bool)
	start := 0
	totalResults := -1
	for {
		// 单次请求页大小：不超过 arXiv 上限；若指定了 -max，最后一页只取剩余量
		page := arxivMaxPerRequest
		if maxResults > 0 {
			if remaining := maxResults - len(papers); remaining < page {
				page = remaining
			}
		}

		q := url.Values{}
		q.Set("search_query", searchQuery)
		q.Set("start", strconv.Itoa(start))
		q.Set("max_results", strconv.Itoa(page))
		q.Set("sortBy", "submittedDate")
		q.Set("sortOrder", "descending")

		feed, err := fetchPage(q)
		if err != nil {
			return nil, err
		}
		if totalResults < 0 {
			totalResults = feed.TotalResults
		}

		for _, e := range feed.Entries {
			p := toPaper(e)
			if p.ArxivID == "" || seen[p.ArxivID] {
				continue
			}
			seen[p.ArxivID] = true
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
		time.Sleep(arxivRequestDelay)
	}
	return papers, nil
}

// fetchPage 发起一次 arXiv API 请求并解析返回的 Feed；5xx/网络错误会重试 3 次。
//
// 若默认路径（按 DNS 直连 export.arxiv.org）连接失败——例如某些网络会对
// export.arxiv.org 解析到的 Fastly IP 直接 RST——会自动回退：用 arxiv.org
// 解析到的 Fastly IP 直连（Fastly 任播同一边缘服务两个域名），并强制 HTTP/1.1。
func fetchPage(q url.Values) (*Feed, error) {
	req, err := http.NewRequest(http.MethodGet, arxivAPI+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "arxiv-ingest/1.0")

	feed, err := doWithClient(req, &http.Client{Timeout: 60 * time.Second})
	if err == nil {
		return feed, nil
	}

	fallbackFeed, fbErr := fetchViaFallbackIPs(req)
	if fbErr == nil {
		return fallbackFeed, nil
	}
	return nil, err // 回退也失败时返回原始错误（更贴近真实原因）
}

// doWithClient 用给定 client 请求并解析 Feed；网络错误/5xx 重试 3 次，4xx 直接返回。
func doWithClient(req *http.Request, client *http.Client) (*Feed, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("arxiv api returned %d: %s", resp.StatusCode, truncate(string(body), 500))
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, lastErr // 4xx 是请求本身的问题，重试无意义
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		var feed Feed
		if err := xml.Unmarshal(body, &feed); err != nil {
			return nil, fmt.Errorf("解析 arXiv XML 失败: %w", err)
		}
		return &feed, nil
	}
	return nil, fmt.Errorf("arxiv api 请求失败(已重试 3 次): %w", lastErr)
}

// fetchViaFallbackIPs 绕过"export.arxiv.org 解析到的 IP 被网络过滤"的问题：
// 用 arxiv.org 解析到的 Fastly IP 直连，TLS SNI 与 HTTP Host 仍保持 export.arxiv.org
// （Fastly 任播集群上同一服务承载这两个域名，已在多处网络实测可通）。
// 同时强制 HTTP/1.1：部分网络对 export.arxiv.org 的 HTTP/2 连接会静默挂起。
func fetchViaFallbackIPs(req *http.Request) (*Feed, error) {
	ips, err := net.LookupHost("arxiv.org")
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("回退解析 arxiv.org 失败: %v", err)
	}
	seen := make(map[string]bool)
	candidates := make([]string, 0, len(ips))
	for _, ip := range ips {
		if strings.Contains(ip, ":") || seen[ip] { // 只走 IPv4 并去重
			continue
		}
		seen[ip] = true
		candidates = append(candidates, ip)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有可用的 arxiv.org IPv4 地址")
	}

	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	var idx int
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		ip := candidates[idx%len(candidates)]
		idx++
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(ip, port))
	}

	transport := &http.Transport{
		DialContext:       dial,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{}, // 禁用 HTTP/2
		ForceAttemptHTTP2: false,
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: transport}
	return doWithClient(req, client)
}

// toPaper 把一条 Atom entry 转成 Paper。
func toPaper(e Entry) Paper {
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
