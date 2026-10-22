package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// sampleFeed 模拟 arXiv API 的真实 Atom 响应（含 opensearch 命名空间）。
const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <title>ArXiv Query: submittedDate:[202406010000 TO 202406012359]</title>
  <opensearch:totalResults>2</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>2</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/2406.00001v1</id>
    <updated>2024-06-01T12:34:56Z</updated>
    <published>2024-06-01T04:00:00Z</published>
    <title>
      A   Test   Title
    </title>
    <summary>
      Summary line 1.
      Summary line 2.
    </summary>
    <author><name>Alice</name></author>
    <author><name>Bob</name></author>
    <category term="cs.CL"/>
    <category term="cs.AI"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2406.00002v1</id>
    <published>2024-06-01T09:00:00Z</published>
    <title>Second Paper</title>
    <summary>Abstract here.</summary>
    <author><name>Carol</name></author>
  </entry>
</feed>`

func TestParseFeed(t *testing.T) {
	var f feed
	if err := xml.Unmarshal([]byte(sampleFeed), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.TotalResults != 2 {
		t.Errorf("TotalResults = %d, want 2", f.TotalResults)
	}
	if len(f.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(f.Entries))
	}

	p := toPaper(f.Entries[0])
	if p.ArxivID != "http://arxiv.org/abs/2406.00001v1" {
		t.Errorf("ArxivID = %q", p.ArxivID)
	}
	if p.Title != "A Test Title" {
		t.Errorf("Title = %q, want %q", p.Title, "A Test Title")
	}
	if p.Abstract != "Summary line 1. Summary line 2." {
		t.Errorf("Abstract = %q", p.Abstract)
	}
	if len(p.Authors) != 2 || p.Authors[0] != "Alice" || p.Authors[1] != "Bob" {
		t.Errorf("Authors = %v", p.Authors)
	}
	if len(p.Categories) != 2 || p.Categories[0] != "cs.CL" || p.Categories[1] != "cs.AI" {
		t.Errorf("Categories = %v", p.Categories)
	}
	if p.Published != "2024-06-01T04:00:00Z" {
		t.Errorf("Published = %q", p.Published)
	}
}

// TestDefaultEndpoint 确认默认走 HTTPS 的 export API（明文 HTTP 不稳定）。
func TestDefaultEndpoint(t *testing.T) {
	if got := NewClient().endpoint; got != "https://export.arxiv.org/api/query" {
		t.Errorf("默认端点 = %q, want HTTPS export 端点", got)
	}
}

// TestRequestParams 验证请求参数：日期区间格式（YYYYMMDDHHMM TO YYYYMMDDHHMM）、
// 分页参数与排序，且空格编码为 + 而非 %2B。
func TestRequestParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"><opensearch:totalResults>0</opensearch:totalResults></feed>`)
	}))
	defer srv.Close()

	c := NewClient(WithEndpoint(srv.URL), WithRequestDelay(0))
	date, _ := time.Parse("2006-01-02", "2024-06-01")
	if _, err := c.FetchSubmitted(context.Background(), date, 0); err != nil {
		t.Fatalf("FetchSubmitted: %v", err)
	}

	if !strings.Contains(gotQuery, "search_query=submittedDate%3A%5B202406010000+TO+202406012359%5D") {
		t.Errorf("search_query 编码不符: %s", gotQuery)
	}
	for _, want := range []string{"start=0", "max_results=2000", "sortBy=submittedDate", "sortOrder=descending"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("缺少参数 %s: %s", want, gotQuery)
		}
	}
	if strings.Contains(gotQuery, "%2B") {
		t.Errorf("空格应编码为 +，而不是 %%2B: %s", gotQuery)
	}
}

// TestFetchSubmittedPagination 用本地 mock 服务器模拟 arXiv 的分页响应，验证：
// 按 start 翻页、用 totalResults 终止、按 maxResults 截断、按 arXiv ID 去重。
func TestFetchSubmittedPagination(t *testing.T) {
	const (
		total    = 5
		pageSize = 2 // 服务端每页最多给 2 条
	)
	mkEntry := func(i int) string {
		return fmt.Sprintf(`<entry>
  <id>http://arxiv.org/abs/2406.%05dv1</id>
  <published>2024-06-01T0%d:00:00Z</published>
  <title>Paper %d</title>
  <summary>Abstract %d</summary>
  <author><name>Author %d</name></author>
</entry>`, i, i%8, i, i, i)
	}

	var starts []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, max int
		fmt.Sscanf(r.URL.Query().Get("start"), "%d", &start)
		fmt.Sscanf(r.URL.Query().Get("max_results"), "%d", &max)
		starts = append(starts, start)
		if max > pageSize {
			max = pageSize
		}

		entries := ""
		for i := start; i < total && i < start+max; i++ {
			entries += mkEntry(i + 1)
		}

		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprintf(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
<opensearch:totalResults>%d</opensearch:totalResults>
<opensearch:startIndex>%d</opensearch:startIndex>
%s</feed>`, total, start, entries)
	}))
	defer srv.Close()

	c := NewClient(WithEndpoint(srv.URL), WithRequestDelay(0))
	date, _ := time.Parse("2006-01-02", "2024-06-01")

	// 1) 不设上限：应跨页取回全部 5 篇，且每页按 start 前进
	all, err := c.FetchSubmitted(context.Background(), date, 0)
	if err != nil {
		t.Fatalf("FetchSubmitted(max=0): %v", err)
	}
	if len(all) != total {
		t.Fatalf("max=0 抓到 %d 篇，want %d", len(all), total)
	}
	wantStarts := []int{0, 2, 4}
	if !slices.Equal(starts, wantStarts) {
		t.Errorf("分页 start 序列 = %v, want %v", starts, wantStarts)
	}

	// 2) 设上限 3：最后一页只取剩余量，总数应被截断
	starts = nil
	three, err := c.FetchSubmitted(context.Background(), date, 3)
	if err != nil {
		t.Fatalf("FetchSubmitted(max=3): %v", err)
	}
	if len(three) != 3 {
		t.Fatalf("max=3 抓到 %d 篇，want 3", len(three))
	}
	if !slices.Equal(starts, []int{0, 2}) {
		t.Errorf("max=3 的 start 序列 = %v, want [0 2]", starts)
	}

	// 3) 同一天重复抓取不会因为 ID 相同而翻倍
	again, err := c.FetchSubmitted(context.Background(), date, 0)
	if err != nil {
		t.Fatalf("second FetchSubmitted: %v", err)
	}
	if len(again) != total {
		t.Fatalf("重复抓取得到 %d 篇，want %d（跨页去重应保持数量）", len(again), total)
	}
}

// TestFetchDropsEntriesWithoutID 没有 arXiv ID 的条目无法作为唯一标识，必须丢弃。
func TestFetchDropsEntriesWithoutID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
<opensearch:totalResults>2</opensearch:totalResults>
<entry><title>没有 ID 的条目</title><summary>should be dropped</summary></entry>
<entry><id>http://arxiv.org/abs/2406.00007v1</id><title>有 ID 的条目</title></entry>
</feed>`)
	}))
	defer srv.Close()

	c := NewClient(WithEndpoint(srv.URL), WithRequestDelay(0))
	papers, err := c.FetchSubmitted(context.Background(), time.Now(), 0)
	if err != nil {
		t.Fatalf("FetchSubmitted: %v", err)
	}
	if len(papers) != 1 || papers[0].ArxivID != "http://arxiv.org/abs/2406.00007v1" {
		t.Fatalf("papers = %+v, want 仅保留带 ID 的条目", papers)
	}
}

// TestFetchSubmittedContextCancel 取消 ctx 后应立即返回，不再继续分页。
func TestFetchSubmittedContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprintf(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
<opensearch:totalResults>100</opensearch:totalResults>
<entry><id>http://arxiv.org/abs/2406.00001v1</id><title>P1</title></entry></feed>`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewClient(WithEndpoint(srv.URL), WithRequestDelay(time.Hour))
	if _, err := c.FetchSubmitted(ctx, time.Now(), 0); err == nil {
		t.Fatal("期望因 ctx 取消而返回错误，实际返回 nil")
	}
}
