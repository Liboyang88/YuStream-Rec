package ingest

import (
	"strings"

	"searcher/arxiv"
	"searcher/domain"
)

// WebSourceArxiv 标识论文来源，与后端约定的 web_source 取值一致。
const WebSourceArxiv = "arxiv"

// ToItems 把 arXiv 抓取结果映射为待入库的 Item。
//
// 关键约定：arXiv ID（Paper.ArxivID，形如 http://arxiv.org/abs/2406.00001v1）
// 写入 Item.Url，它就是入库前的唯一标识（见 domain.Item.Identifier）。
func ToItems(papers []arxiv.Paper) []domain.Item {
	items := make([]domain.Item, 0, len(papers))
	for _, p := range papers {
		items = append(items, domain.Item{
			Title:       p.Title,
			WebSource:   WebSourceArxiv,
			Url:         strings.TrimSpace(p.ArxivID),
			PublishDate: normalizeDate(p.Published),
			Authors:     orEmpty(p.Authors),
			Abstract:    p.Abstract,
			Fields:      orEmpty(p.Categories),
			Keywords:    []string{}, // arXiv 无关键词字段，留空
		})
	}
	return items
}

// normalizeDate 把 arXiv 的 RFC3339 时间截断为 YYYY-MM-DD（与后端一致）。
func normalizeDate(published string) string {
	published = strings.TrimSpace(published)
	if len(published) >= 10 {
		return published[:10]
	}
	return published
}

// orEmpty 保证 JSON 列写入的是 [] 而不是 null。
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
