package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"searcher/arxiv"
	"searcher/domain"
)

// TestMain 关闭日志输出，避免用例里的流程日志淹没测试结果。
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// ---- 测试替身 ----

type fakeSource struct {
	papers []arxiv.Paper
	err    error
}

func (f *fakeSource) FetchSubmitted(context.Context, time.Time, int) ([]arxiv.Paper, error) {
	return f.papers, f.err
}

type fakeRepo struct {
	existing map[string]struct{}

	queriedIDs []string
	savedItems []domain.Item
	savedVecs  [][]float32
	saveErr    error
}

func (f *fakeRepo) ExistingIdentifiers(_ context.Context, ids []string) (map[string]struct{}, error) {
	f.queriedIDs = append(f.queriedIDs, ids...)
	if f.existing == nil {
		return map[string]struct{}{}, nil
	}
	return f.existing, nil
}

func (f *fakeRepo) SaveItems(_ context.Context, items []domain.Item, vectors [][]float32) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.savedItems = append(f.savedItems, items...)
	f.savedVecs = append(f.savedVecs, vectors...)
	return nil
}

type fakeVectorizer struct {
	mu        sync.Mutex
	lastTexts []string
	calls     int
	err       error
}

func (f *fakeVectorizer) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	f.lastTexts = texts
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{float32(i), 1}
	}
	return vecs, nil
}

func (f *fakeVectorizer) Model() string { return "fake-embedding" }
func (f *fakeVectorizer) Dim() int      { return 2 }

func (f *fakeVectorizer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeVectorizer) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTexts
}

// fakeSummarizer / fakeTranslator / fakeVectorizer 都会被多个 goroutine 并发调用
// （富化是并发的），因此计数字段用 atomic、被读的字段用 mutex 保护，保证 -race 下干净。

type fakeSummarizer struct {
	calls     atomic.Int64
	lastTitle atomic.Value // string
	reply     string
	err       error
}

func (f *fakeSummarizer) GenerateSummary(_ context.Context, title, abstract string) (string, error) {
	f.calls.Add(1)
	f.lastTitle.Store(title)
	if f.err != nil {
		return "", f.err
	}
	return f.reply, nil
}

func (f *fakeSummarizer) callCount() int { return int(f.calls.Load()) }

type fakeTranslator struct {
	calls atomic.Int64
	err   error

	inFlight atomic.Int64 // 当前同时在飞的调用数
	peak     atomic.Int64 // 观察到的并发峰值
}

func (f *fakeTranslator) TranslateItem(_ context.Context, title, abstract string) (string, string, error) {
	f.calls.Add(1)
	track := startTrack(&f.inFlight, &f.peak)
	defer track()
	if f.err != nil {
		return "", "", f.err
	}
	return "译文:" + title, "译文:" + abstract, nil
}

func (f *fakeTranslator) callCount() int       { return int(f.calls.Load()) }
func (f *fakeTranslator) peakConcurrency() int { return int(f.peak.Load()) }

// startTrack 记一次进入：inFlight +1 并抬高 peak，返回的函数在退出时把 inFlight 减回去。
func startTrack(inFlight, peak *atomic.Int64) func() {
	cur := inFlight.Add(1)
	for {
		old := peak.Load()
		if cur <= old || peak.CompareAndSwap(old, cur) {
			break
		}
	}
	return func() { inFlight.Add(-1) }
}

func paper(id, title, abstract string) arxiv.Paper {
	return arxiv.Paper{
		ArxivID:    "http://arxiv.org/abs/" + id,
		Title:      title,
		Abstract:   abstract,
		Authors:    []string{"Alice"},
		Categories: []string{"cs.CL"},
		Published:  "2024-06-01T04:00:00Z",
	}
}

func newTestDeps(t *testing.T, papers []arxiv.Paper, existing map[string]struct{}) (*Deps, *fakeRepo, *fakeSummarizer, *fakeTranslator, *fakeVectorizer) {
	t.Helper()
	repo := &fakeRepo{existing: existing}
	sm := &fakeSummarizer{reply: "生成的文献介绍"}
	tr := &fakeTranslator{}
	vec := &fakeVectorizer{}

	deps := &Deps{
		Source:     &fakeSource{papers: papers},
		Summarizer: sm,
		Translator: tr,
		Vectorizer: vec,
		Repository: repo,
	}
	return deps, repo, sm, tr, vec
}

// ---- 用例 ----

// TestRunDedupesByIdentifierBeforeEnriching 是本次改造的核心用例：
// 1) 以 Url（arXiv ID）为准去重，库中已存在的论文被过滤；
// 2) 去重发生在调用 LLM / 翻译之前，已存在的论文不会浪费接口调用；
// 3) 向量化用的是 LLM 生成的 summary（而不是标题）；
// 4) 入参与落库条数一一对应。
func TestRunDedupesByIdentifierBeforeEnriching(t *testing.T) {
	papers := []arxiv.Paper{
		paper("2406.00001v1", "Paper A", "Abstract A"),
		paper("2406.00002v1", "Paper B", "Abstract B"),
		paper("2406.00003v1", "Paper C", "Abstract C"), // 库中已存在
	}
	deps, repo, sm, tr, vec := newTestDeps(t, papers, map[string]struct{}{
		"http://arxiv.org/abs/2406.00003v1": {},
	})

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now(), Max: 0}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 去重查询用的是 Url
	wantIDs := []string{
		"http://arxiv.org/abs/2406.00001v1",
		"http://arxiv.org/abs/2406.00002v1",
		"http://arxiv.org/abs/2406.00003v1",
	}
	if len(repo.queriedIDs) != len(wantIDs) {
		t.Fatalf("查询标识 %v, want %v", repo.queriedIDs, wantIDs)
	}
	for i := range wantIDs {
		if repo.queriedIDs[i] != wantIDs[i] {
			t.Fatalf("查询标识 %v, want %v", repo.queriedIDs, wantIDs)
		}
	}

	// 只对去重后剩下的 2 篇调用 LLM 与翻译
	if sm.callCount() != 2 || tr.callCount() != 2 {
		t.Errorf("LLM 调用次数 summary=%d translate=%d, want 2/2（应先去重再富化）", sm.callCount(), tr.callCount())
	}
	if len(repo.savedItems) != 2 {
		t.Fatalf("落库 %d 篇, want 2", len(repo.savedItems))
	}
	if len(repo.savedVecs) != 2 {
		t.Fatalf("落库向量 %d 条, want 2", len(repo.savedVecs))
	}

	// 向量化用的是 summary，而不是标题（旧实现的变量遮蔽 bug）
	if texts := vec.texts(); len(texts) != 2 || texts[0] != "生成的文献介绍" {
		t.Errorf("向量化文本 = %v, want 均为生成的 summary", texts)
	}

	saved := repo.savedItems[0]
	if saved.Url != "http://arxiv.org/abs/2406.00001v1" {
		t.Errorf("落库标识 = %q, want arXiv ID", saved.Url)
	}
	if saved.WebSource != WebSourceArxiv || saved.PublishDate != "2024-06-01" {
		t.Errorf("落库字段 = %+v", saved)
	}
	if saved.TitleChinese != "译文:Paper A" || saved.AbstractChinese != "译文:Abstract A" {
		t.Errorf("中文字段 = (%q, %q)", saved.TitleChinese, saved.AbstractChinese)
	}
	if saved.IsChinese {
		t.Error("英文论文不应标记为中文")
	}
}

// TestRunFiltersSameTitleDifferentIdentifier 标题相同、标识不同的论文都要入库：
// 旧实现按 title 去重会把它们误判为重复。
func TestRunFiltersSameTitleDifferentIdentifier(t *testing.T) {
	papers := []arxiv.Paper{
		paper("2406.00001v1", "Same Title", "Abstract 1"),
		paper("2406.00002v1", "Same Title", "Abstract 2"),
		paper("2406.00001v1", "Same Title", "Abstract 1 again"), // 标识重复 → 丢弃
	}
	deps, repo, _, _, _ := newTestDeps(t, papers, nil)

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.savedItems) != 2 {
		t.Fatalf("落库 %d 篇, want 2（同标题不同标识应都入库）", len(repo.savedItems))
	}
}

// TestRunAllExisting 全部已存在时不调用 LLM、不写库。
func TestRunAllExisting(t *testing.T) {
	papers := []arxiv.Paper{paper("2406.00001v1", "Paper A", "Abstract A")}
	deps, repo, sm, tr, vec := newTestDeps(t, papers, map[string]struct{}{
		"http://arxiv.org/abs/2406.00001v1": {},
	})

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sm.callCount() != 0 || tr.callCount() != 0 || vec.callCount() != 0 {
		t.Errorf("全部已存在时不应调用外部接口: summary=%d translate=%d embed=%d", sm.callCount(), tr.callCount(), vec.callCount())
	}
	if len(repo.savedItems) != 0 {
		t.Errorf("落库 %d 篇, want 0", len(repo.savedItems))
	}
}

// TestRunDryRun 只打印：不查库、不调用 LLM、不写库。
func TestRunDryRun(t *testing.T) {
	papers := []arxiv.Paper{paper("2406.00001v1", "Paper A", "Abstract A")}
	deps, repo, sm, tr, vec := newTestDeps(t, papers, nil)
	deps.Repository = nil // dry-run 连存储都不需要
	deps.Vectorizer = nil

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now(), DryRun: true}); err != nil {
		t.Fatalf("Run(dry-run): %v", err)
	}
	if sm.callCount() != 0 || tr.callCount() != 0 || vec.callCount() != 0 {
		t.Errorf("dry-run 不应调用外部接口: summary=%d translate=%d embed=%d", sm.callCount(), tr.callCount(), vec.callCount())
	}
	if len(repo.queriedIDs) != 0 {
		t.Errorf("dry-run 不应查询数据库: %v", repo.queriedIDs)
	}
}

// TestRunWithoutStore 非 dry-run 缺少存储时必须报错，而不是静默丢弃数据。
func TestRunWithoutStore(t *testing.T) {
	papers := []arxiv.Paper{paper("2406.00001v1", "Paper A", "Abstract A")}
	deps, _, _, _, _ := newTestDeps(t, papers, nil)
	deps.Repository = nil

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err == nil {
		t.Fatal("缺少存储时期望报错，实际返回 nil")
	}
}

// TestRunSaveError 落库失败要把错误抛给调用方。
func TestRunSaveError(t *testing.T) {
	papers := []arxiv.Paper{paper("2406.00001v1", "Paper A", "Abstract A")}
	deps, repo, _, _, _ := newTestDeps(t, papers, nil)
	repo.saveErr = errors.New("boom")

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err == nil {
		t.Fatal("落库失败时期望报错，实际返回 nil")
	}
}

// TestRunNoPapers 当天没有论文时正常结束。
func TestRunNoPapers(t *testing.T) {
	deps, repo, sm, _, _ := newTestDeps(t, nil, nil)
	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.queriedIDs) != 0 || sm.callCount() != 0 {
		t.Error("没有论文时不应查询数据库或调用 LLM")
	}
}

// TestRunFetchError 抓取失败要原样上报。
func TestRunFetchError(t *testing.T) {
	deps, _, _, _, _ := newTestDeps(t, nil, nil)
	deps.Source = &fakeSource{err: errors.New("network down")}

	runner, err := NewRunner(*deps)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.Run(context.Background(), Request{Date: time.Now()}); err == nil {
		t.Fatal("抓取失败时期望报错，实际返回 nil")
	}
}

// TestEnrichChineseSourceSkipsTranslation 原文已是中文时标记 IsChinese 且不调用翻译。
func TestEnrichChineseSourceSkipsTranslation(t *testing.T) {
	sm := &fakeSummarizer{reply: "中文文献介绍"}
	tr := &fakeTranslator{}
	e := NewEnricher(sm, tr)

	items := []domain.Item{{
		Title:    "一种新的检索方法",
		Abstract: "本文提出了一种新的检索增强生成方法，并在多个数据集上验证了有效性。",
	}}
	e.EnrichAll(context.Background(), items)
	item := items[0]

	if !item.IsChinese {
		t.Error("中文原文应标记 IsChinese = true")
	}
	if tr.callCount() != 0 {
		t.Errorf("中文原文不应调用翻译接口，实际调用 %d 次", tr.callCount())
	}
	if item.Summary != "中文文献介绍" {
		t.Errorf("Summary = %q, want LLM 生成结果", item.Summary)
	}
	if item.TitleChinese != "" || item.AbstractChinese != "" {
		t.Errorf("中文原文不应写入中文字段: (%q, %q)", item.TitleChinese, item.AbstractChinese)
	}
}

// TestEnrichSummaryFallbacks summary 生成失败/未配置时回退为摘要原文，无摘要时回退为标题。
func TestEnrichSummaryFallbacks(t *testing.T) {
	cases := []struct {
		name       string
		summarizer Summarizer
		item       domain.Item
		want       string
	}{
		{
			name:       "生成失败回退摘要",
			summarizer: &fakeSummarizer{err: errors.New("llm down")},
			item:       domain.Item{Title: "T", Abstract: "原文摘要"},
			want:       "原文摘要",
		},
		{
			name:       "生成空串回退摘要",
			summarizer: &fakeSummarizer{reply: "   "},
			item:       domain.Item{Title: "T", Abstract: "原文摘要"},
			want:       "原文摘要",
		},
		{
			name:       "无摘要用标题",
			summarizer: &fakeSummarizer{reply: "不该被调用"},
			item:       domain.Item{Title: "只有标题"},
			want:       "只有标题",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &fakeTranslator{}
			e := NewEnricher(tc.summarizer, tr)
			items := []domain.Item{tc.item}
			e.EnrichAll(context.Background(), items)
			if items[0].Summary != tc.want {
				t.Errorf("Summary = %q, want %q", items[0].Summary, tc.want)
			}
		})
	}
}

// TestEnrichTranslationFailure 翻译失败时只影响中文字段，其余字段照常写入。
func TestEnrichTranslationFailure(t *testing.T) {
	sm := &fakeSummarizer{reply: "介绍"}
	tr := &fakeTranslator{err: errors.New("translate down")}
	e := NewEnricher(sm, tr)

	items := []domain.Item{{Title: "English Title", Abstract: "English abstract."}}
	e.EnrichAll(context.Background(), items)

	if items[0].Summary != "介绍" {
		t.Errorf("Summary = %q, want 介绍", items[0].Summary)
	}
	if items[0].TitleChinese != "" || items[0].AbstractChinese != "" {
		t.Errorf("翻译失败时中文字段应为空: (%q, %q)", items[0].TitleChinese, items[0].AbstractChinese)
	}
}

// TestEnrichAllStopsOnContextCancel ctx 取消（Ctrl-C）后不应继续对剩余条目调用接口。
func TestEnrichAllStopsOnContextCancel(t *testing.T) {
	sm := &fakeSummarizer{reply: "介绍"}
	tr := &fakeTranslator{}
	e := NewEnricher(sm, tr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	items := []domain.Item{
		{Title: "A", Abstract: "Abstract A"},
		{Title: "B", Abstract: "Abstract B"},
		{Title: "C", Abstract: "Abstract C"},
	}
	e.EnrichAll(ctx, items)

	if sm.callCount() != 0 || tr.callCount() != 0 {
		t.Errorf("ctx 已取消时不应调用接口: summary=%d translate=%d", sm.callCount(), tr.callCount())
	}
}

// TestToItems 字段映射：ArxivID → Url、发布时间截断、空切片不回写 null。
func TestToItems(t *testing.T) {
	items := ToItems([]arxiv.Paper{{
		ArxivID:    "  http://arxiv.org/abs/2406.00001v1  ",
		Title:      "T",
		Abstract:   "A",
		Published:  "2024-06-01T04:00:00Z",
		Authors:    nil,
		Categories: nil,
	}})

	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.Url != "http://arxiv.org/abs/2406.00001v1" {
		t.Errorf("Url = %q, want 裁剪后的 arXiv ID", it.Url)
	}
	if it.PublishDate != "2024-06-01" {
		t.Errorf("PublishDate = %q, want 2024-06-01", it.PublishDate)
	}
	if it.WebSource != WebSourceArxiv {
		t.Errorf("WebSource = %q, want %q", it.WebSource, WebSourceArxiv)
	}
	if it.Authors == nil || it.Fields == nil || it.Keywords == nil {
		t.Errorf("JSON 列不应为 nil: %+v", it)
	}
}

// ---- 并发富化相关用例 ----

// countingSummarizer 记录被调用过（进入即计数，不等返回）的条目，用于验证"哪些条目
// 真的被送去调用接口"。block 非 nil 时在计数后阻塞，直到它被关闭——
// 这样调用方可以把若干 goroutine 稳定地"卡在接口里"，制造确定性的并发场景。
type countingSummarizer struct {
	entered atomic.Int64
	reply   string
	block   chan struct{}
}

func (c *countingSummarizer) GenerateSummary(ctx context.Context, title, abstract string) (string, error) {
	c.entered.Add(1)
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return c.reply, nil
}

// TestEnrichAllProcessesEveryItem 并发富化要覆盖每一条：字段逐条写对、顺序无关。
func TestEnrichAllProcessesEveryItem(t *testing.T) {
	const n = 50
	sm := &fakeSummarizer{reply: "生成的文献介绍"}
	tr := &fakeTranslator{}
	e := NewEnricher(sm, tr)

	items := make([]domain.Item, n)
	for i := range items {
		items[i] = domain.Item{
			Title:    fmt.Sprintf("Title %d", i),
			Abstract: fmt.Sprintf("Abstract %d", i),
		}
	}
	e.EnrichAll(context.Background(), items)

	if sm.callCount() != n || tr.callCount() != n {
		t.Fatalf("接口调用次数 summary=%d translate=%d, want %d/%d", sm.callCount(), tr.callCount(), n, n)
	}
	for i := range items {
		wantTitle := fmt.Sprintf("译文:Title %d", i)
		wantAbstract := fmt.Sprintf("译文:Abstract %d", i)
		if items[i].Summary != "生成的文献介绍" {
			t.Fatalf("items[%d].Summary = %q", i, items[i].Summary)
		}
		if items[i].TitleChinese != wantTitle || items[i].AbstractChinese != wantAbstract {
			t.Fatalf("items[%d] 中文字段 = (%q, %q), want (%q, %q)",
				i, items[i].TitleChinese, items[i].AbstractChinese, wantTitle, wantAbstract)
		}
	}
}

// TestEnrichAllHonorsConcurrencyLimit 条目级并发数必须被遵守：同时处理的条目不超过配置值。
func TestEnrichAllHonorsConcurrencyLimit(t *testing.T) {
	const (
		n     = 24
		limit = 4
	)
	sm := &fakeSummarizer{reply: "介绍"}
	tr := &fakeTranslator{}
	e := NewEnricher(sm, tr, WithConcurrency(limit))

	if got := e.Concurrency(); got != limit {
		t.Fatalf("Concurrency() = %d, want %d", got, limit)
	}

	items := make([]domain.Item, n)
	for i := range items {
		items[i] = domain.Item{Title: fmt.Sprintf("T%d", i), Abstract: fmt.Sprintf("A%d", i)}
	}
	// 让翻译慢一点点，制造重叠窗口：并发被限制时峰值应等于 limit。
	slow := &slowTranslator{inner: tr, delay: time.Millisecond}
	e = NewEnricher(sm, slow, WithConcurrency(limit))
	e.EnrichAll(context.Background(), items)

	if tr.callCount() != n {
		t.Fatalf("翻译调用 %d 次, want %d", tr.callCount(), n)
	}
	if peak := slow.peakConcurrency(); peak > limit {
		t.Errorf("翻译并发峰值 = %d, 超过条目级上限 %d", peak, limit)
	}
	if peak := slow.peakConcurrency(); peak < 2 {
		t.Errorf("翻译并发峰值 = %d, 说明并没有真的并发执行", peak)
	}
}

// slowTranslator 给内层翻译加一点延迟，让并发调用有机会重叠（否则瞬时完成测不出并发）。
type slowTranslator struct {
	inner Translator
	delay time.Duration

	inFlight atomic.Int64
	peak     atomic.Int64
}

func (s *slowTranslator) TranslateItem(ctx context.Context, title, abstract string) (string, string, error) {
	track := startTrack(&s.inFlight, &s.peak)
	defer track()
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
	return s.inner.TranslateItem(ctx, title, abstract)
}

func (s *slowTranslator) peakConcurrency() int { return int(s.peak.Load()) }

// TestEnrichDefaultConcurrency 未配置并发数时使用默认值；非法值（0 / 负数）也回退到默认值。
func TestEnrichDefaultConcurrency(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
	}{
		{"未指定", nil},
		{"0", []Option{WithConcurrency(0)}},
		{"负数", []Option{WithConcurrency(-3)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEnricher(nil, nil, tc.opts...)
			if got := e.Concurrency(); got != DefaultConcurrency {
				t.Errorf("Concurrency() = %d, want %d", got, DefaultConcurrency)
			}
		})
	}
}

// TestEnrichSingleItemRunsStepsConcurrently 单条内部的"生成介绍"与"翻译"应并发：
// 两者各自阻塞，若串行执行就会一直等到接口超时，并发则总耗时接近单次阻塞时长。
func TestEnrichSingleItemRunsStepsConcurrently(t *testing.T) {
	const delay = 200 * time.Millisecond

	sm := &blockingSummarizer{reply: "介绍", delay: delay}
	tr := &blockingTranslator{delay: delay}
	e := NewEnricher(sm, tr)

	items := []domain.Item{{Title: "Title", Abstract: "Abstract"}}
	start := time.Now()
	e.EnrichAll(context.Background(), items)
	elapsed := time.Since(start)

	if items[0].Summary != "介绍" {
		t.Errorf("Summary = %q", items[0].Summary)
	}
	if items[0].TitleChinese != "译文:Title" {
		t.Errorf("TitleChinese = %q", items[0].TitleChinese)
	}
	// 串行需要约 2*delay；并发应显著快于它。留足余量避免 CI 抖动导致偶发失败。
	if elapsed >= 2*delay-50*time.Millisecond {
		t.Errorf("单条富化耗时 %v，接近串行耗时 %v，说明两步没有并发", elapsed, 2*delay)
	}
}

// blockingSummarizer 睡 delay 后返回，用于测量并发带来的耗时差异。
type blockingSummarizer struct {
	reply string
	delay time.Duration
}

func (b *blockingSummarizer) GenerateSummary(ctx context.Context, title, abstract string) (string, error) {
	select {
	case <-time.After(b.delay):
		return b.reply, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// blockingTranslator 同样睡 delay 后返回。
type blockingTranslator struct{ delay time.Duration }

func (b *blockingTranslator) TranslateItem(ctx context.Context, title, abstract string) (string, string, error) {
	select {
	case <-time.After(b.delay):
		return "译文:" + title, "译文:" + abstract, nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// TestEnrichAllCancelStopsSchedulingRemaining 运行中取消（Ctrl-C）：已开始的条目让它收尾，
// 剩余条目不再发起接口调用。用"卡在接口里"的摘要器制造确定性时序。
func TestEnrichAllCancelStopsSchedulingRemaining(t *testing.T) {
	const (
		n     = 20
		limit = 3
	)

	ctx, cancel := context.WithCancel(context.Background())
	sm := &countingSummarizer{reply: "介绍", block: make(chan struct{})}
	tr := &fakeTranslator{}
	e := NewEnricher(sm, tr, WithConcurrency(limit))

	items := make([]domain.Item, n)
	for i := range items {
		items[i] = domain.Item{Title: fmt.Sprintf("T%d", i), Abstract: fmt.Sprintf("A%d", i)}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.EnrichAll(ctx, items)
	}()

	// 等到 limit 个 goroutine 都卡在接口里：此时并发名额已用满，其余条目都在等名额。
	waitFor(t, time.Second, func() bool { return sm.entered.Load() == limit })

	// 取消后放开阻塞的调用，让在飞的条目正常收尾。
	cancel()
	close(sm.block)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("EnrichAll 在 ctx 取消后没有及时返回")
	}

	if got := sm.entered.Load(); got != limit {
		t.Errorf("取消后仍对 %d 条调用了接口, want %d（其余条目应不再发起请求）", got, limit)
	}
	if got := tr.callCount(); got != limit {
		t.Errorf("翻译调用 %d 次, want %d（与已开始的条目数一致）", got, limit)
	}
}

// waitFor 轮询直到 cond 为真或超时，用于等待并发时序到达预期状态。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待条件超时")
}

// TestEnrichAllEmpty 空切片不应panic，也不应调用接口。
func TestEnrichAllEmpty(t *testing.T) {
	sm := &fakeSummarizer{reply: "介绍"}
	e := NewEnricher(sm, &fakeTranslator{}, WithConcurrency(4))
	e.EnrichAll(context.Background(), nil)
	if sm.callCount() != 0 {
		t.Errorf("空输入不应调用接口，实际 %d 次", sm.callCount())
	}
}
