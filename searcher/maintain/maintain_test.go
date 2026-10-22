package maintain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"searcher/store"
)

// ---- 测试替身 ----

type fakeRepo struct {
	collection string
	count      store.ItemCount
	dates      []store.DateCount
	vectors    int64

	purgeResult store.PurgeResult
	purgeErr    error
	purgeCalls  int

	// delete-date 相关
	dateCounts     map[string]int64
	countByDateErr error
	deletedDates   []string
	deleteResult   store.DateDeleteResult
	deleteErr      error

	countErr   error
	datesErr   error
	vectorsErr error
}

func (f *fakeRepo) Collection() string { return f.collection }

func (f *fakeRepo) CountItems(context.Context) (store.ItemCount, error) {
	return f.count, f.countErr
}

func (f *fakeRepo) ListItemDates(context.Context) ([]store.DateCount, error) {
	return f.dates, f.datesErr
}

func (f *fakeRepo) VectorCount(context.Context) (int64, error) {
	return f.vectors, f.vectorsErr
}

func (f *fakeRepo) CountItemsByDate(_ context.Context, publishDate string) (int64, error) {
	if f.countByDateErr != nil {
		return 0, f.countByDateErr
	}
	return f.dateCounts[publishDate], nil
}

func (f *fakeRepo) DeleteByDate(_ context.Context, publishDate string) (store.DateDeleteResult, error) {
	f.deletedDates = append(f.deletedDates, publishDate)
	if f.deleteErr != nil {
		return store.DateDeleteResult{}, f.deleteErr
	}
	if f.deleteResult.PublishDate == "" {
		return store.DateDeleteResult{PublishDate: publishDate, MySQLDeleted: f.dateCounts[publishDate]}, nil
	}
	return f.deleteResult, nil
}

func (f *fakeRepo) PurgeAll(context.Context) (store.PurgeResult, error) {
	f.purgeCalls++
	if f.purgeErr != nil {
		return store.PurgeResult{}, f.purgeErr
	}
	return f.purgeResult, nil
}

// captureOutput 同时捕获 stdout 与 log（stderr）的输出。
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	oldLogWriter := log.Writer()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdout = w
	log.SetOutput(w)

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = oldStdout
	log.SetOutput(oldLogWriter)
	out := <-done
	_ = r.Close()
	return out
}

func newRunner(t *testing.T, repo Repository) *Runner {
	t.Helper()
	runner, err := NewRunner(repo)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// ---- 用例 ----

// TestStats 报告 MySQL 与 Milvus 两边的数量，并在不一致时给出提示。
func TestStats(t *testing.T) {
	cases := []struct {
		name       string
		count      store.ItemCount
		vectors    int64
		wantOutput []string
	}{
		{
			name:       "数量一致",
			count:      store.ItemCount{Total: 579},
			vectors:    579,
			wantOutput: []string{"MySQL items 表：579 条记录（其中已软删除 0 条）", "Milvus 集合 litstream：579 条向量"},
		},
		{
			name:       "有软删除行",
			count:      store.ItemCount{Total: 10, Deleted: 3},
			vectors:    10,
			wantOutput: []string{"MySQL items 表：10 条记录（其中已软删除 3 条）"},
		},
		{
			name:       "MySQL 更多（缺向量）",
			count:      store.ItemCount{Total: 10},
			vectors:    7,
			wantOutput: []string{"MySQL 比 Milvus 多 3 条"},
		},
		{
			name:       "Milvus 更多（孤儿向量）",
			count:      store.ItemCount{Total: 7},
			vectors:    10,
			wantOutput: []string{"Milvus 比 MySQL 多 3 条向量"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{collection: "litstream", count: tc.count, vectors: tc.vectors}
			out := captureOutput(t, func() {
				if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpStats}); err != nil {
					t.Errorf("Run: %v", err)
				}
			})
			for _, want := range tc.wantOutput {
				if !strings.Contains(out, want) {
					t.Errorf("输出缺少 %q，实际输出:\n%s", want, out)
				}
			}
			if repo.purgeCalls != 0 {
				t.Error("stats 不应调用 PurgeAll")
			}
		})
	}
}

// TestDates 按日期输出条数，并给出合计；没有记录时明确提示。
func TestDates(t *testing.T) {
	repo := &fakeRepo{
		collection: "litstream",
		dates: []store.DateCount{
			{PublishDate: "2026-08-01", Total: 579},
			{PublishDate: "2024-06-01", Total: 12, Deleted: 2},
		},
	}
	out := captureOutput(t, func() {
		if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpDates}); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	for _, want := range []string{"publish_date", "2026-08-01", "579", "2024-06-01", "共 2 个日期，591 条记录（其中已软删除 2 条）"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q，实际输出:\n%s", want, out)
		}
	}

	// 空表
	empty := &fakeRepo{collection: "litstream"}
	out = captureOutput(t, func() {
		if err := newRunner(t, empty).Run(context.Background(), Request{Op: OpDates}); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "没有任何 item 记录") {
		t.Errorf("空表时应提示没有记录，实际输出:\n%s", out)
	}
}

// TestPurgeRequiresConfirmation 未加确认时必须拒绝执行，且不能碰数据库。
func TestPurgeRequiresConfirmation(t *testing.T) {
	repo := &fakeRepo{collection: "litstream", count: store.ItemCount{Total: 579}, vectors: 579}
	err := newRunner(t, repo).Run(context.Background(), Request{Op: OpPurge, Confirmed: false})
	if err == nil {
		t.Fatal("未确认时期望报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "-yes") {
		t.Errorf("错误信息应提示加 -yes，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "579") {
		t.Errorf("错误信息应包含将被删除的规模，实际: %v", err)
	}
	if repo.purgeCalls != 0 {
		t.Errorf("未确认时不应执行清空，实际调用 %d 次", repo.purgeCalls)
	}
}

// TestPurgeConfirmed 确认后执行清空，并报告删除规模。
func TestPurgeConfirmed(t *testing.T) {
	repo := &fakeRepo{
		collection:  "litstream",
		count:       store.ItemCount{Total: 579},
		vectors:     579,
		purgeResult: store.PurgeResult{MySQLDeleted: 579, MilvusVectors: 579, Collection: "litstream"},
	}
	out := captureOutput(t, func() {
		if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpPurge, Confirmed: true}); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	if repo.purgeCalls != 1 {
		t.Fatalf("PurgeAll 调用 %d 次, want 1", repo.purgeCalls)
	}
	if !strings.Contains(out, "清空完成：MySQL 删除 579 条") {
		t.Errorf("输出缺少清空结果，实际输出:\n%s", out)
	}
}

// TestPurgeFailureSurfacesError 清空失败要把错误抛给调用方，并提示可能只完成了一部分。
func TestPurgeFailureSurfacesError(t *testing.T) {
	repo := &fakeRepo{
		collection: "litstream",
		purgeErr:   errors.New("milvus down"),
	}
	err := newRunner(t, repo).Run(context.Background(), Request{Op: OpPurge, Confirmed: true})
	if err == nil {
		t.Fatal("清空失败时期望报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "milvus down") || !strings.Contains(err.Error(), "复核") {
		t.Errorf("错误信息不完整: %v", err)
	}
}

// TestUnknownOp 未知操作要报错并列出可选项。
func TestUnknownOp(t *testing.T) {
	repo := &fakeRepo{collection: "litstream"}
	err := newRunner(t, repo).Run(context.Background(), Request{Op: Op("drop-everything")})
	if err == nil {
		t.Fatal("未知操作期望报错，实际返回 nil")
	}
	for _, want := range []string{"stats", "dates", "purge"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应包含可选操作 %q，实际: %v", want, err)
		}
	}
}

// TestErrorsPropagate 查询失败时原样上报，不吞错。
func TestErrorsPropagate(t *testing.T) {
	repo := &fakeRepo{collection: "litstream", countErr: errors.New("mysql down")}
	if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpStats}); err == nil {
		t.Error("CountItems 失败时 stats 应报错")
	}

	repo = &fakeRepo{collection: "litstream", datesErr: errors.New("mysql down")}
	if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpDates}); err == nil {
		t.Error("ListItemDates 失败时 dates 应报错")
	}

	repo = &fakeRepo{collection: "litstream", vectorsErr: errors.New("milvus down")}
	if err := newRunner(t, repo).Run(context.Background(), Request{Op: OpStats}); err == nil {
		t.Error("VectorCount 失败时 stats 应报错")
	}
}

// TestNewRunnerRequiresRepo 没有 Repository 时构造失败。
func TestNewRunnerRequiresRepo(t *testing.T) {
	if _, err := NewRunner(nil); err == nil {
		t.Fatal("缺少 Repository 时期望报错，实际返回 nil")
	}
}

// ---- delete-date ----

// TestDeleteDateRequiresConfirmation 未加 -yes 时只报告规模并拒绝执行，绝不删数据。
func TestDeleteDateRequiresConfirmation(t *testing.T) {
	repo := &fakeRepo{
		collection: "litstream_items",
		dateCounts: map[string]int64{"2024-06-20": 42},
	}
	runner := newRunner(t, repo)

	err := runner.Run(context.Background(), Request{Op: OpDeleteDate, Date: "2024-06-20"})
	if err == nil {
		t.Fatal("未确认时期望报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "42") || !strings.Contains(err.Error(), "-yes") {
		t.Errorf("错误信息应包含条数与 -yes 提示，实际: %v", err)
	}
	if len(repo.deletedDates) != 0 {
		t.Errorf("未确认时不应调用删除，实际删除日期: %v", repo.deletedDates)
	}
}

// TestDeleteDateConfirmed 确认后删除该日期，并报告 MySQL / Milvus 两侧的删除数。
func TestDeleteDateConfirmed(t *testing.T) {
	repo := &fakeRepo{
		collection:   "litstream_items",
		dateCounts:   map[string]int64{"2024-06-20": 42},
		deleteResult: store.DateDeleteResult{PublishDate: "2024-06-20", MySQLDeleted: 42, MilvusDeleted: 42},
	}
	runner := newRunner(t, repo)

	var err error
	out := captureOutput(t, func() {
		err = runner.Run(context.Background(), Request{Op: OpDeleteDate, Date: "2024-06-20", Confirmed: true})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.deletedDates) != 1 || repo.deletedDates[0] != "2024-06-20" {
		t.Errorf("删除日期 = %v, want [2024-06-20]", repo.deletedDates)
	}
	for _, want := range []string{"2024-06-20", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q，实际: %s", want, out)
		}
	}
}

// TestDeleteDateEmptyDay 该日期没有记录时正常结束（不是错误），也不调用删除。
func TestDeleteDateEmptyDay(t *testing.T) {
	repo := &fakeRepo{collection: "c", dateCounts: map[string]int64{}}
	runner := newRunner(t, repo)

	var err error
	out := captureOutput(t, func() {
		err = runner.Run(context.Background(), Request{Op: OpDeleteDate, Date: "2020-01-01", Confirmed: true})
	})
	if err != nil {
		t.Fatalf("空日期不应报错: %v", err)
	}
	if len(repo.deletedDates) != 0 {
		t.Errorf("没有记录时不应调用删除: %v", repo.deletedDates)
	}
	if !strings.Contains(out, "没有记录") {
		t.Errorf("应提示没有记录，实际: %s", out)
	}
}

// TestDeleteDateValidatesDate 日期缺失或格式非法要在调用存储之前就拒绝。
func TestDeleteDateValidatesDate(t *testing.T) {
	cases := []struct {
		name string
		date string
	}{
		{"缺失", ""},
		{"空白", "   "},
		{"格式错误", "2024/06/20"},
		{"不存在的日期", "2024-02-31"},
		{"月份非法", "2024-13-01"},
		{"只有年月", "2024-06"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{collection: "c", dateCounts: map[string]int64{"x": 1}}
			runner := newRunner(t, repo)

			err := runner.Run(context.Background(), Request{Op: OpDeleteDate, Date: tc.date, Confirmed: true})
			if err == nil {
				t.Fatal("非法日期期望报错，实际返回 nil")
			}
			if len(repo.deletedDates) != 0 {
				t.Errorf("非法日期不应触达存储层，实际删除: %v", repo.deletedDates)
			}
		})
	}
}

// TestDeleteDateErrorPropagates 删除失败要上报，并提示可能只完成了一部分。
func TestDeleteDateErrorPropagates(t *testing.T) {
	repo := &fakeRepo{
		collection: "c",
		dateCounts: map[string]int64{"2024-06-20": 5},
		deleteErr:  errors.New("milvus down"),
	}
	runner := newRunner(t, repo)

	err := runner.Run(context.Background(), Request{Op: OpDeleteDate, Date: "2024-06-20", Confirmed: true})
	if err == nil {
		t.Fatal("删除失败期望报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "复核") {
		t.Errorf("错误信息应提示用 -op stats/-op dates 复核，实际: %v", err)
	}
}

// TestValidDate 直接校验日期解析规则（含闰日）。
func TestValidDate(t *testing.T) {
	cases := map[string]bool{
		"2024-06-20": true,
		"2024-02-29": true,  // 闰年
		"2023-02-29": false, // 非闰年
		"2024-2-9":   false, // 未补零
		"2024-02-31": false,
		"":           false,
	}
	for date, want := range cases {
		if got := validDate(date); got != want {
			t.Errorf("validDate(%q) = %v, want %v", date, got, want)
		}
	}
}

// TestOpsIncludesDeleteDate Ops() 用于帮助文本，必须包含新操作。
func TestOpsIncludesDeleteDate(t *testing.T) {
	found := false
	for _, op := range Ops() {
		if op == OpDeleteDate {
			found = true
		}
	}
	if !found {
		t.Errorf("Ops() = %v, 缺少 %s", Ops(), OpDeleteDate)
	}
}
