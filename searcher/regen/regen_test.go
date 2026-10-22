package regen

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"searcher/domain"
	"searcher/storefields"
)

// ---- 测试替身 ----

// fakeRepo 记录所有回写调用，并可按日期返回预设记录。
type fakeRepo struct {
	items []domain.Item
	err   error

	mu           sync.Mutex
	translations []storefields.TranslationUpdate
	summaries    []storefields.SummaryUpdate
	vectors      []storefields.VectorUpdate
	listCalls    int
}

func (f *fakeRepo) ListItemsByDate(ctx context.Context, publishDate string) ([]domain.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func (f *fakeRepo) UpdateTranslations(ctx context.Context, updates []storefields.TranslationUpdate) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.translations = append(f.translations, updates...)
	return int64(len(updates)), nil
}

func (f *fakeRepo) UpdateSummaries(ctx context.Context, updates []storefields.SummaryUpdate) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summaries = append(f.summaries, updates...)
	return int64(len(updates)), nil
}

func (f *fakeRepo) ReplaceVectors(ctx context.Context, updates []storefields.VectorUpdate) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vectors = append(f.vectors, updates...)
	return int64(len(updates)), nil
}

// fakeTranslator 按调用次数返回可控结果。
type fakeTranslator struct {
	mu       sync.Mutex
	calls    int
	failOn   map[string]bool // 标题命中则失败
	received []string
}

func (f *fakeTranslator) TranslateItem(ctx context.Context, title, abstract string) (string, string, error) {
	f.mu.Lock()
	f.calls++
	f.received = append(f.received, title)
	fail := f.failOn[title]
	f.mu.Unlock()

	if fail {
		return "", "", errors.New("翻译接口挂了")
	}
	return "译:" + title, "译:" + abstract, nil
}

func (f *fakeTranslator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeSummarizer 对指定标题返回错误，其余返回可预期的文本。
type fakeSummarizer struct {
	mu     sync.Mutex
	calls  int
	failOn map[string]bool
	empty  map[string]bool
}

func (f *fakeSummarizer) GenerateSummary(ctx context.Context, title, abstract string) (string, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.failOn[title] {
		return "", errors.New("摘要模型挂了")
	}
	if f.empty[title] {
		return "   ", nil
	}
	return "摘要:" + title, nil
}

// fakeVectorizer 返回可区分的向量，便于校验顺序与归位。
type fakeVectorizer struct {
	mu       sync.Mutex
	inputs   []string
	failWith error
	dim      int
}

func (f *fakeVectorizer) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, texts...)
	if f.failWith != nil {
		return nil, f.failWith
	}
	// 每条向量第一个分量编码下标，便于断言"输入与向量一一对应"。
	out := make([][]float32, 0, len(texts))
	for i := range texts {
		vec := make([]float32, f.dim)
		vec[0] = float32(i + 1)
		out = append(out, vec)
	}
	return out, nil
}

func (f *fakeVectorizer) Model() string { return "fake-embedding" }
func (f *fakeVectorizer) Dim() int      { return f.dim }

// item 构造一条测试记录。
func item(id uint, title, abstract, summary string) domain.Item {
	return domain.Item{ID: id, Title: title, Abstract: abstract, Summary: summary, PublishDate: "2024-06-20"}
}

// testDeps 返回一套齐备的依赖与记录。
func testDeps(items ...domain.Item) (*Runner, *fakeRepo, *fakeTranslator, *fakeSummarizer, *fakeVectorizer) {
	repo := &fakeRepo{items: items}
	tr := &fakeTranslator{}
	sm := &fakeSummarizer{}
	vec := &fakeVectorizer{dim: 4}

	r, err := NewRunner(Deps{Repository: repo, Translator: tr, Summarizer: sm, Vectorizer: vec, Concurrency: 4})
	if err != nil {
		panic(err)
	}
	return r, repo, tr, sm, vec
}

// ---- 参数与确认 ----

// TestRunRequiresDate 三个操作都必须带 -date。
func TestRunRequiresDate(t *testing.T) {
	for _, op := range []Op{OpTranslate, OpSummary, OpEmbedding} {
		r, _, tr, sm, vec := testDeps()
		err := r.Run(context.Background(), Request{Op: op, Date: "  ", Confirmed: true})
		if err == nil {
			t.Errorf("%s：缺少日期时期望报错", op)
		}
		if !strings.Contains(err.Error(), "-date") {
			t.Errorf("%s：错误提示应告诉用户加 -date，实际: %v", op, err)
		}
		// 缺日期时不该调用任何接口
		if tr.callCount() != 0 || sm.calls != 0 || len(vec.inputs) != 0 {
			t.Errorf("%s：参数校验失败时不该调用外部接口", op)
		}
	}
}

// TestRunRequiresConfirmation 未加 -yes 时只报告规模，不调用接口、不写库。
func TestRunRequiresConfirmation(t *testing.T) {
	r, repo, tr, sm, vec := testDeps(item(1, "A", "abs", "s"), item(2, "B", "abs", "s"))

	err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20"})
	if err == nil {
		t.Fatal("未确认时期望报错")
	}
	if !strings.Contains(err.Error(), "-yes") {
		t.Errorf("错误提示应告诉用户加 -yes，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "2 条") {
		t.Errorf("错误提示应给出记录条数，实际: %v", err)
	}
	if tr.callCount() != 0 || sm.calls != 0 || len(vec.inputs) != 0 {
		t.Error("未确认时不该调用外部接口")
	}
	if len(repo.translations)+len(repo.summaries)+len(repo.vectors) != 0 {
		t.Error("未确认时不该写库")
	}
}

// TestRunEmptyDate 该日期没有记录时直接返回，不报错、不写库。
func TestRunEmptyDate(t *testing.T) {
	r, repo, _, _, _ := testDeps()
	if err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("无记录时不该报错: %v", err)
	}
	if len(repo.translations) != 0 {
		t.Error("无记录时不该写库")
	}
}

// TestRunUnknownOp 未知操作报错，且提示可选值。
func TestRunUnknownOp(t *testing.T) {
	r, _, _, _, _ := testDeps(item(1, "A", "abs", "s"))
	err := r.Run(context.Background(), Request{Op: Op("nope"), Date: "2024-06-20", Confirmed: true})
	if err == nil {
		t.Fatal("未知操作期望报错")
	}
	if !strings.Contains(err.Error(), "translate") {
		t.Errorf("错误提示应列出可选操作，实际: %v", err)
	}
}

// TestMissingCapability 对应能力未配置时直接报错，且提示缺哪个键。
func TestMissingCapability(t *testing.T) {
	cases := []struct {
		name    string
		deps    Deps
		op      Op
		wantKey string
	}{
		{"没有翻译", Deps{Repository: &fakeRepo{items: []domain.Item{item(1, "A", "b", "c")}}}, OpTranslate, "ALIYUN_MT"},
		{"没有摘要", Deps{Repository: &fakeRepo{items: []domain.Item{item(1, "A", "b", "c")}}}, OpSummary, "SUMMARY_MODEL"},
		{"没有 embedding", Deps{Repository: &fakeRepo{items: []domain.Item{item(1, "A", "b", "c")}}}, OpEmbedding, "EMBEDDING_MODEL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRunner(tc.deps)
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			err = r.Run(context.Background(), Request{Op: tc.op, Date: "2024-06-20", Confirmed: true})
			if err == nil {
				t.Fatal("能力缺失时期望报错")
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("错误提示应提到 %s，实际: %v", tc.wantKey, err)
			}
		})
	}
}

// TestNewRunnerRequiresRepository Repository 是硬依赖。
func TestNewRunnerRequiresRepository(t *testing.T) {
	if _, err := NewRunner(Deps{}); err == nil {
		t.Fatal("缺少 Repository 时期望报错")
	}
}

// ---- translate ----

// TestRegenTranslate 重新翻译：写入标题与摘要译文，并带上 is_chinese 标记。
func TestRegenTranslate(t *testing.T) {
	r, repo, tr, _, _ := testDeps(
		item(1, "Paper One", "Abstract one", "summary"),
		item(2, "Paper Two", "Abstract two", "summary"),
	)

	err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := tr.callCount(); got != 2 {
		t.Errorf("翻译调用 %d 次, want 2", got)
	}
	if len(repo.translations) != 2 {
		t.Fatalf("回写 %d 条, want 2", len(repo.translations))
	}

	byID := map[uint]storefields.TranslationUpdate{}
	for _, u := range repo.translations {
		byID[u.ID] = u
	}
	if u := byID[1]; u.TitleChinese != "译:Paper One" || u.AbstractChinese != "译:Abstract one" {
		t.Errorf("id=1 译文 = (%q, %q)", u.TitleChinese, u.AbstractChinese)
	}
	if u := byID[2]; u.TitleChinese != "译:Paper Two" {
		t.Errorf("id=2 标题译文 = %q", u.TitleChinese)
	}
}

// TestRegenTranslateSkipsChinese 原文已是中文的记录不调用翻译接口，只标记 is_chinese。
func TestRegenTranslateSkipsChinese(t *testing.T) {
	r, repo, tr, _, _ := testDeps(
		item(1, "中文标题", "这是一段中文摘要，用于测试语种判断是否生效。", "summary"),
		item(2, "English Title", "An English abstract.", "summary"),
	)

	if err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := tr.callCount(); got != 1 {
		t.Errorf("翻译调用 %d 次, want 1（中文那条应跳过）", got)
	}
	if len(repo.translations) != 2 {
		t.Fatalf("回写 %d 条, want 2（两条都要回写 is_chinese）", len(repo.translations))
	}
	for _, u := range repo.translations {
		if u.ID == 1 {
			if !u.IsChinese {
				t.Error("id=1 是中文原文，is_chinese 应为 true")
			}
			if u.TitleChinese != "" {
				t.Errorf("id=1 不该有译文，实际 %q", u.TitleChinese)
			}
		}
		if u.ID == 2 && u.IsChinese {
			t.Error("id=2 是英文原文，is_chinese 应为 false")
		}
	}
}

// TestRegenTranslatePartialFailure 单条失败只跳过该条，其余照常回写。
func TestRegenTranslatePartialFailure(t *testing.T) {
	r, repo, tr, _, _ := testDeps(
		item(1, "Good", "abs", "s"),
		item(2, "Bad", "abs", "s"),
		item(3, "Also Good", "abs", "s"),
	)
	tr.failOn = map[string]bool{"Bad": true}

	if err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("部分失败不该让整体报错: %v", err)
	}
	if len(repo.translations) != 2 {
		t.Fatalf("回写 %d 条, want 2（失败的那条不写）", len(repo.translations))
	}
	for _, u := range repo.translations {
		if u.ID == 2 {
			t.Error("失败的记录不该被回写")
		}
	}
}

// TestRegenTranslateAllFail 全部失败时报错，且不写任何记录。
func TestRegenTranslateAllFail(t *testing.T) {
	r, repo, tr, _, _ := testDeps(item(1, "A", "abs", "s"), item(2, "B", "abs", "s"))
	tr.failOn = map[string]bool{"A": true, "B": true}

	if err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err == nil {
		t.Fatal("全部失败时期望报错")
	}
	if len(repo.translations) != 0 {
		t.Errorf("全部失败时不该写库，实际写了 %d 条", len(repo.translations))
	}
}

// TestRegenTranslateOrderIndependent 回写顺序不影响结果（并发完成顺序不确定）。
func TestRegenTranslateOrderIndependent(t *testing.T) {
	items := make([]domain.Item, 0, 20)
	for i := 1; i <= 20; i++ {
		items = append(items, item(uint(i), "T"+string(rune('A'+i-1)), "abs", "s"))
	}
	r, repo, _, _, _ := testDeps(items...)

	if err := r.Run(context.Background(), Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.translations) != 20 {
		t.Fatalf("回写 %d 条, want 20", len(repo.translations))
	}
	// 每条回写的译文必须与它自己的 ID 对应，不能串位。
	for _, u := range repo.translations {
		want := "译:T" + string(rune('A'+u.ID-1))
		if u.TitleChinese != want {
			t.Errorf("id=%d 标题译文 = %q, want %q（可能串位）", u.ID, u.TitleChinese, want)
		}
	}
}

// ---- summary ----

// TestRegenSummary 重新生成 summary。
func TestRegenSummary(t *testing.T) {
	r, repo, _, sm, _ := testDeps(
		item(1, "Paper One", "Abstract one", "旧 summary"),
		item(2, "Paper Two", "Abstract two", "旧 summary"),
	)

	if err := r.Run(context.Background(), Request{Op: OpSummary, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sm.calls != 2 {
		t.Errorf("摘要调用 %d 次, want 2", sm.calls)
	}
	byID := map[uint]string{}
	for _, u := range repo.summaries {
		byID[u.ID] = u.Summary
	}
	if byID[1] != "摘要:Paper One" || byID[2] != "摘要:Paper Two" {
		t.Errorf("summary 回写 = %v", byID)
	}
}

// TestRegenSummaryFallbackOnFailure 失败时回退为摘要原文（与 ingest 口径一致），
// 不会把已有 summary 变成空串。
func TestRegenSummaryFallbackOnFailure(t *testing.T) {
	r, repo, _, sm, _ := testDeps(
		item(1, "Good", "Abstract one", "旧"),
		item(2, "Bad", "Abstract two", "旧"),
	)
	sm.failOn = map[string]bool{"Bad": true}

	if err := r.Run(context.Background(), Request{Op: OpSummary, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.summaries) != 2 {
		t.Fatalf("回写 %d 条, want 2（失败的也要写回退值）", len(repo.summaries))
	}
	for _, u := range repo.summaries {
		if u.ID == 2 && u.Summary != "Abstract two" {
			t.Errorf("id=2 应回退为摘要原文，实际 %q", u.Summary)
		}
		if u.Summary == "" {
			t.Errorf("id=%d 的 summary 不该为空", u.ID)
		}
	}
}

// TestRegenSummaryFallbackOnEmpty 模型返回空内容同样回退。
func TestRegenSummaryFallbackOnEmpty(t *testing.T) {
	r, repo, _, sm, _ := testDeps(item(1, "Empty", "Abstract text", "旧"))
	sm.empty = map[string]bool{"Empty": true}

	if err := r.Run(context.Background(), Request{Op: OpSummary, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.summaries) != 1 || repo.summaries[0].Summary != "Abstract text" {
		t.Errorf("空内容应回退为摘要原文，实际 %+v", repo.summaries)
	}
}

// TestRegenSummaryWithoutAbstract 没有摘要时不调用模型，直接用回退值（标题）。
func TestRegenSummaryWithoutAbstract(t *testing.T) {
	r, repo, _, sm, _ := testDeps(item(1, "Only Title", "   ", "旧"))

	if err := r.Run(context.Background(), Request{Op: OpSummary, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sm.calls != 0 {
		t.Errorf("没有摘要时不该调用模型，实际调用 %d 次", sm.calls)
	}
	if len(repo.summaries) != 1 || repo.summaries[0].Summary != "Only Title" {
		t.Errorf("应回退为标题，实际 %+v", repo.summaries)
	}
}

// ---- embedding ----

// TestRegenEmbeddingUsesSummary 向量化输入是当前的 summary。
func TestRegenEmbeddingUsesSummary(t *testing.T) {
	r, repo, _, _, vec := testDeps(
		item(1, "T1", "A1", "新的 summary 一"),
		item(2, "T2", "A2", "新的 summary 二"),
	)

	if err := r.Run(context.Background(), Request{Op: OpEmbedding, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(vec.inputs) != 2 {
		t.Fatalf("embedding 输入 %d 条, want 2", len(vec.inputs))
	}
	if vec.inputs[0] != "新的 summary 一" || vec.inputs[1] != "新的 summary 二" {
		t.Errorf("embedding 输入 = %v, want 各条的 summary", vec.inputs)
	}
	if len(repo.vectors) != 2 {
		t.Fatalf("替换 %d 条向量, want 2", len(repo.vectors))
	}
	// 向量必须与记录一一对应（第 i 条向量第 0 分量 = i+1）
	for _, u := range repo.vectors {
		if int(u.Vec[0]) != int(u.ID) {
			t.Errorf("id=%d 拿到了第 %v 条向量，可能串位", u.ID, u.Vec[0])
		}
	}
}

// TestRegenEmbeddingFallsBackWithoutSummary summary 为空时用"摘要原文、其次标题"兜底，
// 避免把空串编码成无意义的向量（与 ingest 的 fallbackSummary 同源）。
func TestRegenEmbeddingFallsBackWithoutSummary(t *testing.T) {
	r, _, _, _, vec := testDeps(
		item(1, "Title One", "Abstract One", "   "), // summary 空 → 用摘要原文
		item(2, "Title Two", "   ", "   "),          // 摘要也空 → 用标题
	)

	if err := r.Run(context.Background(), Request{Op: OpEmbedding, Date: "2024-06-20", Confirmed: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(vec.inputs) != 2 {
		t.Fatalf("embedding 输入 %d 条, want 2", len(vec.inputs))
	}
	if vec.inputs[0] != "Abstract One" {
		t.Errorf("inputs[0] = %q, want 摘要原文兜底", vec.inputs[0])
	}
	if vec.inputs[1] != "Title Two" {
		t.Errorf("inputs[1] = %q, want 标题兜底", vec.inputs[1])
	}
}

// TestRegenEmbeddingFailureAborts embedding 失败时不该写任何向量。
func TestRegenEmbeddingFailureAborts(t *testing.T) {
	r, repo, _, _, vec := testDeps(item(1, "T", "A", "S"))
	vec.failWith = errors.New("embedding 服务挂了")

	if err := r.Run(context.Background(), Request{Op: OpEmbedding, Date: "2024-06-20", Confirmed: true}); err == nil {
		t.Fatal("embedding 失败时期望报错")
	}
	if len(repo.vectors) != 0 {
		t.Errorf("embedding 失败时不该写向量，实际写了 %d 条", len(repo.vectors))
	}
}

// TestRegenEmbeddingCountMismatch 返回数量不符时必须报错（否则 ID 与向量会错配）。
func TestRegenEmbeddingCountMismatch(t *testing.T) {
	repo := &fakeRepo{items: []domain.Item{item(1, "T1", "A", "S"), item(2, "T2", "A", "S")}}
	// 返回 1 条向量的 vectorizer
	short := &shortVectorizer{}
	r, err := NewRunner(Deps{Repository: repo, Vectorizer: short})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if err := r.Run(context.Background(), Request{Op: OpEmbedding, Date: "2024-06-20", Confirmed: true}); err == nil {
		t.Fatal("数量不符时期望报错")
	}
	if len(repo.vectors) != 0 {
		t.Error("数量不符时不该写向量")
	}
}

// shortVectorizer 故意少返回一条向量。
type shortVectorizer struct{}

func (s *shortVectorizer) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return [][]float32{{1, 2, 3, 4}}, nil
}
func (s *shortVectorizer) Model() string { return "short" }
func (s *shortVectorizer) Dim() int      { return 4 }

// ---- 其他 ----

// TestListErrorPropagates 查询失败时原样上报。
func TestListErrorPropagates(t *testing.T) {
	r, repo, _, _, _ := testDeps()
	repo.err = errors.New("数据库连不上")

	err := r.Run(context.Background(), Request{Op: OpSummary, Date: "2024-06-20", Confirmed: true})
	if err == nil {
		t.Fatal("查询失败时期望报错")
	}
	if !strings.Contains(err.Error(), "数据库连不上") {
		t.Errorf("应保留原始错误，实际: %v", err)
	}
}

// TestConcurrencyDefault 未指定并发时用默认值，显式值保持原样。
func TestConcurrencyDefault(t *testing.T) {
	r, err := NewRunner(Deps{Repository: &fakeRepo{}})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if r.Concurrency() != DefaultConcurrency {
		t.Errorf("默认并发 = %d, want %d", r.Concurrency(), DefaultConcurrency)
	}

	r2, err := NewRunner(Deps{Repository: &fakeRepo{}, Concurrency: 3})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if r2.Concurrency() != 3 {
		t.Errorf("显式并发 = %d, want 3", r2.Concurrency())
	}
}

// TestCancelledContextStopsEarly ctx 已取消时不再调用接口。
func TestCancelledContextStopsEarly(t *testing.T) {
	r, _, tr, _, _ := testDeps(item(1, "A", "abs", "s"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx, Request{Op: OpTranslate, Date: "2024-06-20", Confirmed: true}); err == nil {
		t.Fatal("ctx 已取消时期望报错")
	}
	if tr.callCount() != 0 {
		t.Errorf("ctx 已取消时不该调用翻译接口，实际 %d 次", tr.callCount())
	}
}
