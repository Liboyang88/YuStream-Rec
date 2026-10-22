// Package regen 按日期重新生成已有记录的派生字段，用于模型更换后的数据刷新：
//
//	translate  重新生成 title_chinese / abstract_chinese（换了机器翻译后重译）
//	summary    重新生成 summary（换了摘要模型后重算）
//	embedding  重新生成 Milvus 里的向量（换了 embedding 模型后重算）
//
// 与 ingest 的分工：ingest 只处理"库里还没有的"论文（先按标识去重再富化），
// 因此换模型后**已入库的记录永远不会被刷新**。本包补的就是这一块：按 publish_date
// 把已有记录捞出来，重算指定字段再回写。
//
// 三个操作的依赖关系（注意顺序）：
//
//	summary ──► embedding
//
// 向量是对 summary 文本编码得到的，所以换了摘要模型后必须
// **先 regen-summary、再 regen-embedding**，否则向量编码的还是旧 summary。
// translate 与另外两个互不影响，随时可跑。
package regen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"searcher/domain"
	"searcher/lang"
	"searcher/semaphore"
	"searcher/storefields"
)

// ChineseRatioThreshold 与 ingest 侧保持一致：判定"原文已是中文"的汉字占比阈值。
// 复制常量而不是 import ingest，避免为一个阈值引入包依赖。
const ChineseRatioThreshold = 0.5

// DefaultConcurrency 是重新生成时同时处理的记录数（与 ingest.DefaultConcurrency 一致）。
const DefaultConcurrency = 8

// Op 是一次重新生成操作的类型。
type Op string

const (
	OpTranslate Op = "translate" // 重新生成中文翻译
	OpSummary   Op = "summary"   // 重新生成文献介绍
	OpEmbedding Op = "embedding" // 重新生成向量
)

// String 便于日志与错误信息里直接打印。
func (o Op) String() string { return string(o) }

// Summarizer 生成文献介绍（由 llm.Summarizer 实现）。
type Summarizer interface {
	GenerateSummary(ctx context.Context, title, abstract string) (string, error)
}

// Translator 翻译标题与摘要（由 translator.Client 实现）。
type Translator interface {
	TranslateItem(ctx context.Context, title, abstract string) (titleZh, abstractZh string, err error)
}

// Vectorizer 把文本编码为向量（由 embedder.Embedder 实现）。
type Vectorizer interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model / Dim 仅用于日志输出。
	Model() string
	Dim() int
}

// Repository 是重新生成需要的存储能力。
type Repository interface {
	// ListItemsByDate 取出该日期的全部记录（含原始 title/abstract）。
	ListItemsByDate(ctx context.Context, publishDate string) ([]domain.Item, error)
	UpdateTranslations(ctx context.Context, updates []storefields.TranslationUpdate) (int64, error)
	UpdateSummaries(ctx context.Context, updates []storefields.SummaryUpdate) (int64, error)
	ReplaceVectors(ctx context.Context, updates []storefields.VectorUpdate) (int64, error)
}

// Deps 是 Runner 的依赖集合。三个能力各自可为空，表示未配置（对应操作会直接报错）。
type Deps struct {
	Repository Repository
	Translator Translator
	Summarizer Summarizer
	Vectorizer Vectorizer
	// Concurrency 是同时处理的记录数；<= 0 时用 DefaultConcurrency。
	// 各接口自己的并发上限由客户端闸门控制，与这里无关。
	Concurrency int
}

// Request 描述一次重新生成请求。
type Request struct {
	Op Op
	// Date 是要处理的 publish_date（YYYY-MM-DD）。
	Date string
	// Confirmed 为假时只报告将要处理多少条，不调用任何接口、不写库。
	// 重新生成会覆盖已有字段并消耗接口额度，误敲日期代价不小。
	Confirmed bool
}

// Runner 执行重新生成。
type Runner struct {
	repo        Repository
	translator  Translator
	summarizer  Summarizer
	vectorizer  Vectorizer
	concurrency int
}

// NewRunner 构造 Runner。
func NewRunner(deps Deps) (*Runner, error) {
	if deps.Repository == nil {
		return nil, errors.New("regen: 必须提供 Repository")
	}
	concurrency := deps.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	return &Runner{
		repo:        deps.Repository,
		translator:  deps.Translator,
		summarizer:  deps.Summarizer,
		vectorizer:  deps.Vectorizer,
		concurrency: concurrency,
	}, nil
}

// Concurrency 返回条目级并发数（用于日志与测试）。
func (r *Runner) Concurrency() int { return r.concurrency }

// Run 执行一次重新生成。
func (r *Runner) Run(ctx context.Context, req Request) error {
	date := strings.TrimSpace(req.Date)
	if date == "" {
		return fmt.Errorf("必须提供 -date（YYYY-MM-DD），例如 -op regen-%s -date 2024-06-20", req.Op)
	}

	// 先确认所需能力已配置，避免白查一遍数据库才报错。
	if err := r.checkCapability(req.Op); err != nil {
		return err
	}

	items, err := r.repo.ListItemsByDate(ctx, date)
	if err != nil {
		return err
	}
	// 查完立刻看 ctx：ctx 已取消时 gorm 会返回"空结果 + nil 错误"，
	// 直接往下走会打印"没有记录，无需重新生成"并以 0 退出——
	// 看起来像跑成功了，实际一条都没处理。
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("查询被中断: %w", err)
	}
	if len(items) == 0 {
		fmt.Printf("%s 没有记录，无需重新生成\n", date)
		return nil
	}

	if !req.Confirmed {
		return fmt.Errorf(
			"%s 有 %d 条记录将重新生成 %s，会覆盖已有字段并消耗接口额度；确认执行请加 -yes",
			date, len(items), opDescription(req.Op),
		)
	}

	switch req.Op {
	case OpTranslate:
		return r.runTranslate(ctx, date, items)
	case OpSummary:
		return r.runSummary(ctx, date, items)
	case OpEmbedding:
		return r.runEmbedding(ctx, date, items)
	default:
		return fmt.Errorf("未知操作 %q，可选：%s / %s / %s", req.Op, OpTranslate, OpSummary, OpEmbedding)
	}
}

// checkCapability 校验该操作依赖的能力是否已配置。
func (r *Runner) checkCapability(op Op) error {
	switch op {
	case OpTranslate:
		if r.translator == nil {
			return errors.New("未配置机器翻译（ALIYUN_MT_ACCESS_KEY_ID / _SECRET），无法重新生成翻译")
		}
	case OpSummary:
		if r.summarizer == nil {
			return errors.New("未配置摘要模型（SUMMARY_MODEL_NAME / SUMMARY_MODEL_API_KEY），无法重新生成文献介绍")
		}
	case OpEmbedding:
		if r.vectorizer == nil {
			return errors.New("未配置 embedding 服务（EMBEDDING_MODEL_*），无法重新生成向量")
		}
	default:
		return fmt.Errorf("未知操作 %q，可选：%s / %s / %s", op, OpTranslate, OpSummary, OpEmbedding)
	}
	return nil
}

// opDescription 返回操作的中文说明，用于确认提示与日志。
func opDescription(op Op) string {
	switch op {
	case OpTranslate:
		return "中文翻译（title_chinese / abstract_chinese）"
	case OpSummary:
		return "文献介绍（summary）"
	case OpEmbedding:
		return "向量（Milvus 中该日期的向量）"
	default:
		return string(op)
	}
}

// runTranslate 重新翻译中文标题与摘要。
//
// 原文本身就是中文的记录不做翻译（与 ingest 一致：调用接口只会得到原样返回），
// 只把 is_chinese 标记为真，然后一起回写。
func (r *Runner) runTranslate(ctx context.Context, date string, items []domain.Item) error {
	log.Printf("%s：重新生成中文翻译（%d 条，并发 %d）...", date, len(items), r.concurrency)

	updates, errs := mapConcurrently(ctx, items, r.concurrency, func(ctx context.Context, item domain.Item) (storefields.TranslationUpdate, error) {
		update := storefields.TranslationUpdate{ID: item.ID}

		if lang.IsMostlyChinese(sourceText(item), ChineseRatioThreshold) {
			update.IsChinese = true
			return update, nil
		}

		titleZh, abstractZh, err := r.translator.TranslateItem(ctx, item.Title, item.Abstract)
		if err != nil {
			return update, fmt.Errorf("翻译失败: %w", err)
		}
		update.TitleChinese = titleZh
		update.AbstractChinese = abstractZh
		return update, nil
	})

	ok, failed := splitResults(updates, errs)
	logFailures("翻译", items, errs)

	if len(ok) == 0 {
		return fmt.Errorf("全部 %d 条翻译都失败，未写入任何记录", len(items))
	}
	written, err := r.repo.UpdateTranslations(ctx, ok)
	if err != nil {
		return err
	}
	log.Printf("%s：翻译重新生成完成，成功 %d 条、失败 %d 条，MySQL 更新 %d 行", date, len(ok), failed, written)
	return nil
}

// runSummary 重新生成文献介绍。
//
// 与 ingest.buildSummary 的降级口径一致：模型失败或返回空内容时回退为"摘要原文，
// 其次标题"。这样重新生成不会把已有 summary 变成空串。
//
// 与 translate / embedding 的关键区别：**回退值也是要写的**。那两条失败时没有可写
// 的值（译文未知、向量未知），只能跳过；而这里失败条目仍有一个可用的 summary，
// 丢掉它反而会让该条的 summary 继续停在旧模型的产物上。代价是回退值会覆盖原本正常的
// summary，所以失败条目逐条打日志，便于确认后重跑。
func (r *Runner) runSummary(ctx context.Context, date string, items []domain.Item) error {
	log.Printf("%s：重新生成文献介绍（%d 条，并发 %d）...", date, len(items), r.concurrency)

	updates, errs := mapConcurrently(ctx, items, r.concurrency, func(ctx context.Context, item domain.Item) (storefields.SummaryUpdate, error) {
		fallback := fallbackSummary(item)

		// 没有摘要时没有可概括的素材，直接用回退值，省掉一次模型调用。
		if strings.TrimSpace(item.Abstract) == "" {
			return storefields.SummaryUpdate{ID: item.ID, Summary: fallback}, nil
		}

		summary, err := r.summarizer.GenerateSummary(ctx, item.Title, item.Abstract)
		if err != nil {
			return storefields.SummaryUpdate{ID: item.ID, Summary: fallback}, fmt.Errorf("生成失败（已回退为摘要原文）: %w", err)
		}
		if strings.TrimSpace(summary) == "" {
			return storefields.SummaryUpdate{ID: item.ID, Summary: fallback}, errors.New("模型返回空内容（已回退为摘要原文）")
		}
		return storefields.SummaryUpdate{ID: item.ID, Summary: strings.TrimSpace(summary)}, nil
	})

	failed := countErrors(errs)
	logFailures("文献介绍", items, errs)

	// ctx 取消导致没跑的条目结果为零值，不能写进去覆盖旧 summary。
	writable := nonZeroIDs(updates)
	if len(writable) == 0 {
		return fmt.Errorf("%s 的 %d 条文献介绍都没有产出可用结果，未写入任何记录", date, len(items))
	}
	written, err := r.repo.UpdateSummaries(ctx, writable)
	if err != nil {
		return err
	}
	log.Printf("%s：文献介绍重新生成完成，模型生成 %d 条、回退 %d 条，MySQL 更新 %d 行",
		date, len(writable)-failed, failed, written)
	if failed > 0 {
		log.Printf("提示：%d 条回退成了摘要原文；若期望模型结果，可重跑 -op regen-summary -date %s -yes", failed, date)
	}
	return nil
}

// runEmbedding 重新生成向量：对当前 summary 编码，替换 Milvus 里的旧向量。
//
// summary 为空时用 title 兜底（与富化时的 fallbackSummary 同源），避免把空串编码
// 成一个无意义的向量。注意这里用的是**库里当前的 summary**——若刚换过摘要模型，
// 应先跑 regen-summary，否则编码的还是旧 summary。
func (r *Runner) runEmbedding(ctx context.Context, date string, items []domain.Item) error {
	log.Printf("%s：重新生成向量（%d 条，模型=%s，维度=%d）...",
		date, len(items), r.vectorizer.Model(), r.vectorizer.Dim())

	vectors, err := r.vectorizer.Embed(ctx, embeddingTexts(items))
	if err != nil {
		return fmt.Errorf("embedding 失败: %w", err)
	}
	if len(vectors) != len(items) {
		return fmt.Errorf("embedding 返回数量不符: %d 条输入, 返回 %d 条向量", len(items), len(vectors))
	}

	updates := make([]storefields.VectorUpdate, 0, len(items))
	for i, item := range items {
		updates = append(updates, storefields.VectorUpdate{ID: item.ID, Vec: vectors[i]})
	}

	written, err := r.repo.ReplaceVectors(ctx, updates)
	if err != nil {
		return err
	}
	log.Printf("%s：向量重新生成完成，替换 %d 条", date, written)
	return nil
}

// embeddingTexts 返回各条记录参与向量化的文本：优先 summary，为空时退回标题。
func embeddingTexts(items []domain.Item) []string {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(item.Summary); s != "" {
			texts = append(texts, s)
			continue
		}
		texts = append(texts, fallbackSummary(item))
	}
	return texts
}

// fallbackSummary 兜底文本：优先摘要原文，其次标题（与 ingest 侧口径一致）。
func fallbackSummary(item domain.Item) string {
	if s := strings.TrimSpace(item.Abstract); s != "" {
		return s
	}
	return item.Title
}

// sourceText 返回用于语种判断的原文：优先摘要，摘要为空时用标题。
func sourceText(item domain.Item) string {
	if s := strings.TrimSpace(item.Abstract); s != "" {
		return s
	}
	return item.Title
}

// mapConcurrently 以最多 concurrency 个 goroutine 把 items 映射为结果。
//
// 每个下标由它自己的 goroutine 独占写入，父 goroutine 等全部完成后才返回，
// 因此调用方随后读结果切片不会与写入竞争（与 ingest 的富化并发同一套保证）。
// 单条失败只记录在该下标的 error 上，不影响其余条目。
func mapConcurrently[T any](ctx context.Context, items []domain.Item, concurrency int, fn func(context.Context, domain.Item) (T, error)) ([]T, []error) {
	results := make([]T, len(items))
	errs := make([]error, len(items))

	var (
		wg    sync.WaitGroup
		slots = semaphore.New(concurrency)
	)
	for i := range items {
		wg.Add(1)
		go func(idx int, item domain.Item) {
			defer wg.Done()
			if err := slots.Acquire(ctx); err != nil {
				errs[idx] = err // ctx 已取消，不再对这条发起接口调用
				return
			}
			defer slots.Release()

			out, err := fn(ctx, item)
			results[idx] = out
			errs[idx] = err
		}(i, items[i])
	}
	wg.Wait()
	return results, errs
}

// splitResults 把"结果 + 错误"拆成"成功写回的"与失败条数。
//
// 只有**没有报错**的条目才进入写回列表：报错意味着这条没有产出可信的新值
// （ctx 取消也一样），写回去只会用零值覆盖库里的旧数据。需要"失败也写回退值"的
// 操作（summary）不走这里，改用 nonZeroIDs。
func splitResults[T any](results []T, errs []error) ([]T, int) {
	ok := make([]T, 0, len(results))
	failed := 0
	for i := range results {
		if errs[i] != nil {
			failed++
			continue
		}
		ok = append(ok, results[i])
	}
	return ok, failed
}

// countErrors 统计失败的条目数。
func countErrors(errs []error) int {
	n := 0
	for _, err := range errs {
		if err != nil {
			n++
		}
	}
	return n
}

// nonZeroIDs 挑出真正产出了结果的条目（ID 非 0）。
//
// 用于 summary：失败条目也带着回退值（ID 已填）要写回，但被 ctx 取消而根本没跑的
// 条目结果仍是零值（ID == 0），必须剔除——否则会拿空 summary 覆盖库里已有的数据。
// 主键从 1 开始自增，因此 ID == 0 就是"没有结果"的可靠标记。
func nonZeroIDs(updates []storefields.SummaryUpdate) []storefields.SummaryUpdate {
	out := make([]storefields.SummaryUpdate, 0, len(updates))
	for _, u := range updates {
		if u.ID == 0 {
			continue
		}
		out = append(out, u)
	}
	return out
}

// logFailures 打印失败条目的标识与原因，便于定位与重跑。
// 只打印前几条，避免一次失败上千条时刷屏。
func logFailures(what string, items []domain.Item, errs []error) {
	const maxLogged = 5

	logged := 0
	for i := range errs {
		if errs[i] == nil {
			continue
		}
		if logged < maxLogged {
			log.Printf("%s失败: id=%d title=%q err=%v", what, items[i].ID, items[i].Title, errs[i])
		}
		logged++
	}
	if logged > maxLogged {
		log.Printf("%s失败共 %d 条，此处只列出前 %d 条", what, logged, maxLogged)
	}
}
