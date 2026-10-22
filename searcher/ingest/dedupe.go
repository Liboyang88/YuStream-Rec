package ingest

import "searcher/domain"

// Stats 描述一次去重的结果。
//
// 口径说明：一个标识若在库中已存在，则同一批里所有携带该标识的条目都计入
// AlreadyStored（它们都是"库里已有"的重复），因此 DuplicateInBatch 与
// AlreadyStored 可能针对同一个标识分别计数——两者相加可能大于被丢弃的条目总数
// 的情况并不存在，但同一个标识不会被同时计入两项。
type Stats struct {
	Fetched           int // 去重前的条目数
	Kept              int // 去重后保留的条目数
	DuplicateInBatch  int // 同一批内标识重复而被丢弃的条目数
	AlreadyStored     int // 标识已存在于库中而被过滤的条目数
	WithoutIdentifier int // 没有标识、无法判定而保留的条目数
}

// KeepNew 按唯一标识（Item.Identifier，即 item.Url / paper.ArxivID）过滤：
//
//  1. 同一批内标识重复时只保留第一条（抓取分页或多次抓取都可能带来重复）；
//  2. 标识出现在 existing（库中已存在）时整条过滤掉。
//
// 标识为空的条目无法判定是否重复，直接保留，避免误丢数据。
//
// 返回值是新分配的切片，不会复用/覆盖入参 items 的底层数组。
func KeepNew(items []domain.Item, existing map[string]struct{}) ([]domain.Item, Stats) {
	kept := make([]domain.Item, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	stats := Stats{Fetched: len(items)}

	for _, it := range items {
		id := it.Identifier()
		if id == "" {
			stats.WithoutIdentifier++
			kept = append(kept, it)
			continue
		}
		if _, dup := seen[id]; dup {
			stats.DuplicateInBatch++
			continue
		}
		if _, ok := existing[id]; ok {
			stats.AlreadyStored++
			continue
		}
		seen[id] = struct{}{}
		kept = append(kept, it)
	}

	stats.Kept = len(kept)
	return kept, stats
}

// Identifiers 收集各条目的唯一标识。
func Identifiers(items []domain.Item) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Identifier())
	}
	return ids
}
