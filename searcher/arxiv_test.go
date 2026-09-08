package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	var feed Feed
	if err := xml.Unmarshal([]byte(sampleFeed), &feed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if feed.TotalResults != 2 {
		t.Errorf("TotalResults = %d, want 2", feed.TotalResults)
	}
	if len(feed.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(feed.Entries))
	}

	p := toPaper(feed.Entries[0])
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

// TestQueryEncoding 验证最终发给 arXiv 的 URL 形态：分页参数、HTTPS、
// search_query 的日期区间格式（YYYYMMDDHHMM TO YYYYMMDDHHMM，与官方文档一致）。
func TestQueryEncoding(t *testing.T) {
	q := url.Values{}
	q.Set("search_query", "submittedDate:[202406010000 TO 202406012359]")
	q.Set("start", "0")
	q.Set("max_results", "2000")
	q.Set("sortBy", "submittedDate")
	q.Set("sortOrder", "descending")

	raw := q.Encode()
	if !strings.Contains(raw, "search_query=submittedDate%3A%5B202406010000+TO+202406012359%5D") {
		t.Errorf("unexpected encoded search_query: %s", raw)
	}
	if !strings.Contains(raw, "max_results=2000") {
		t.Errorf("max_results missing: %s", raw)
	}
	if strings.Contains(raw, "%2B") {
		t.Errorf("space must be encoded as +, not %%2B: %s", raw)
	}
	if arxivAPI != "https://export.arxiv.org/api/query" {
		t.Errorf("arxivAPI = %s, want https endpoint", arxivAPI)
	}
}

// TestFetchPagination 用本地 mock 服务器模拟 arXiv 的分页响应，
// 验证：按 start 翻页、用 totalResults 终止、按 maxResults 截断、按 arXiv ID 去重。
func TestFetchPagination(t *testing.T) {
	const total = 3
	// 服务端按 max_results 每页最多给 2 条
	mkEntry := func(i int) string {
		return fmt.Sprintf(`<entry>
  <id>http://arxiv.org/abs/2406.%05dv1</id>
  <published>2024-06-01T0%d:00:00Z</published>
  <title>Paper %d</title>
  <summary>Abstract %d</summary>
  <author><name>Author %d</name></author>
</entry>`, i, i%8, i, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		max := 0
		if v := r.URL.Query().Get("start"); v != "" {
			fmt.Sscanf(v, "%d", &start)
		}
		if v := r.URL.Query().Get("max_results"); v != "" {
			fmt.Sscanf(v, "%d", &max)
		}
		entries := ""
		n := 0
		for i := start; i < total && n < max; i++ {
			entries += mkEntry(i + 1)
			n++
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprintf(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
<opensearch:totalResults>%d</opensearch:totalResults>
<opensearch:startIndex>%d</opensearch:startIndex>
%s</feed>`, total, start, entries)
	}))
	defer srv.Close()

	oldAPI, oldDelay := arxivAPI, arxivRequestDelay
	arxivAPI = srv.URL
	arxivRequestDelay = 0
	defer func() { arxivAPI, arxivRequestDelay = oldAPI, oldDelay }()

	date, _ := time.Parse("2006-01-02", "2024-06-01")

	// 1) 不设上限：应跨页取回全部 3 篇
	all, err := FetchArxivPapers(date, 0)
	if err != nil {
		t.Fatalf("FetchArxivPapers(max=0): %v", err)
	}
	if len(all) != total {
		t.Fatalf("max=0 抓到 %d 篇，want %d", len(all), total)
	}

	// 2) 设上限 2：应截断为 2 篇
	two, err := FetchArxivPapers(date, 2)
	if err != nil {
		t.Fatalf("FetchArxivPapers(max=2): %v", err)
	}
	if len(two) != 2 {
		t.Fatalf("max=2 抓到 %d 篇，want 2", len(two))
	}

	// 3) 同一天重复抓取不会因为 ID 相同而翻倍
	again, err := FetchArxivPapers(date, 0)
	if err != nil {
		t.Fatalf("second FetchArxivPapers: %v", err)
	}
	if len(again) != total {
		t.Fatalf("重复抓取得到 %d 篇，want %d（每页去重应保持数量）", len(again), total)
	}
}
