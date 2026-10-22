package ingest

import (
	"testing"

	"searcher/domain"
)

// TestKeepNew 验证去重口径：唯一标识是 item.Url（= paper.ArxivID），
// 而不是标题——标题相同但标识不同的论文必须都保留。
func TestKeepNew(t *testing.T) {
	items := []domain.Item{
		{Title: "Paper A", Url: "http://arxiv.org/abs/2406.00001v1"},
		{Title: "Paper B", Url: "http://arxiv.org/abs/2406.00002v1"},
		// 批内重复：同一标识出现两次，只保留第一条
		{Title: "Paper A (re-announced)", Url: "http://arxiv.org/abs/2406.00001v1"},
		// 库中已存在（标识命中）
		{Title: "Paper C", Url: "http://arxiv.org/abs/2406.00003v1"},
		// 标识带首尾空白，裁剪后与已存在的标识相同：同样是"库中已存在"而被过滤
		{Title: "Paper C (whitespace)", Url: "  http://arxiv.org/abs/2406.00003v1  "},
		// 标题与 Paper A 相同但标识不同：属于不同论文，必须保留
		{Title: "Paper A", Url: "http://arxiv.org/abs/2406.00009v1"},
		// 没有标识：无法判定重复，保留
		{Title: "Paper D", Url: ""},
	}

	existing := map[string]struct{}{
		"http://arxiv.org/abs/2406.00003v1": {},
	}

	original := make([]domain.Item, len(items))
	copy(original, items)

	kept, stats := KeepNew(items, existing)

	wantTitles := []string{"Paper A", "Paper B", "Paper A", "Paper D"}
	if len(kept) != len(wantTitles) {
		t.Fatalf("保留 %d 篇，want %d: %+v", len(kept), len(wantTitles), kept)
	}
	for i, want := range wantTitles {
		if kept[i].Title != want {
			t.Errorf("保留第 %d 篇 = %q, want %q", i, kept[i].Title, want)
		}
	}

	wantStats := Stats{Fetched: 7, Kept: 4, DuplicateInBatch: 1, AlreadyStored: 2, WithoutIdentifier: 1}
	if stats != wantStats {
		t.Errorf("stats = %+v, want %+v", stats, wantStats)
	}

	// 不能就地覆盖入参（旧实现对入参做了 items[:0] 复用，调用方拿到的数据会被破坏）。
	if len(items) != len(original) {
		t.Fatalf("入参切片长度被改动: %d, want %d", len(items), len(original))
	}
	for i := range original {
		if items[i].Title != original[i].Title || items[i].Url != original[i].Url {
			t.Errorf("入参第 %d 条被就地修改: %+v, want %+v", i, items[i], original[i])
		}
	}
}

// TestKeepNewNilExisting 库里没有任何记录时，只做批内去重。
func TestKeepNewNilExisting(t *testing.T) {
	items := []domain.Item{
		{Title: "A", Url: "id-1"},
		{Title: "B", Url: "id-1"},
		{Title: "C", Url: "id-2"},
	}
	kept, stats := KeepNew(items, nil)
	if len(kept) != 2 {
		t.Fatalf("保留 %d 篇，want 2", len(kept))
	}
	if stats.DuplicateInBatch != 1 || stats.AlreadyStored != 0 {
		t.Errorf("stats = %+v, want 批内重复 1、库中 0", stats)
	}
}

// TestKeepNewEmpty 空输入不 panic。
func TestKeepNewEmpty(t *testing.T) {
	kept, stats := KeepNew(nil, nil)
	if len(kept) != 0 || stats.Fetched != 0 || stats.Kept != 0 {
		t.Fatalf("kept = %v, stats = %+v", kept, stats)
	}
}

// TestIdentifiers 标识收集会裁剪空白。
func TestIdentifiers(t *testing.T) {
	ids := Identifiers([]domain.Item{
		{Url: " a "},
		{Url: "b"},
		{Url: ""},
	})
	want := []string{"a", "b", ""}
	if len(ids) != len(want) {
		t.Fatalf("Identifiers = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("Identifiers[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}
