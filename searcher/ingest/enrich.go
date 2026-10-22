package ingest

import (
	"context"
	"log"
	"strings"
	"sync"

	"searcher/domain"
	"searcher/lang"
	"searcher/semaphore"
)

// ChineseRatioThreshold 是判定"原文已是中文"的汉字占比阈值。
const ChineseRatioThreshold = 0.5

// DefaultConcurrency 是未显式配置时的条目级并发数。
//
// 取值理由：一篇论文要发两次接口调用（文献介绍 + 翻译），8 条并发意味着同一时刻
// 最多约 16 个请求在飞。再往上加收益递减（瓶颈转到接口侧的排队与限流），却更容易
// 触发 429；再往下则本地等待明显。
const DefaultConcurrency = 8

// Summarizer 根据标题与摘要生成简短的文献介绍。
type Summarizer interface {
	GenerateSummary(ctx context.Context, title, abstract string) (string, error)
}

// Translator 把标题与摘要翻译成中文。
type Translator interface {
	TranslateItem(ctx context.Context, title, abstract string) (titleZh, abstractZh string, err error)
}

// Enricher 为 Item 补齐 summary 与中文字段。
// 两个依赖都可以为 nil，表示该项能力未配置（跳过对应步骤）。
//
// 条目之间并发处理（并发数由 Enricher 持有），单条内部的"生成介绍"与"翻译"也并发：
// 两者调用的是不同服务（千问 MaaS / 阿里云机器翻译），互不干涉，合起来能把单条等待
// 时间压掉近一半。各接口的并发上限由客户端自己的闸门控制
// （见 llm.WithSemaphore / translator.WithSemaphore）。
type Enricher struct {
	summarizer  Summarizer
	translator  Translator
	concurrency int
}

// Option 覆盖 Enricher 的默认行为。
type Option func(*Enricher)

// WithConcurrency 设置同时处理的条目数。n <= 0 时退回 DefaultConcurrency。
func WithConcurrency(n int) Option {
	return func(e *Enricher) { e.concurrency = n }
}

// NewEnricher 构造富化器；未指定 WithConcurrency 时使用 DefaultConcurrency。
func NewEnricher(summarizer Summarizer, translator Translator, opts ...Option) *Enricher {
	e := &Enricher{summarizer: summarizer, translator: translator, concurrency: DefaultConcurrency}
	for _, opt := range opts {
		opt(e)
	}
	if e.concurrency <= 0 {
		e.concurrency = DefaultConcurrency
	}
	return e
}

// Concurrency 返回条目级并发数（用于日志与测试）。
func (e *Enricher) Concurrency() int { return e.concurrency }

// EnrichAll 并发补齐所有条目；单条失败只降级该条，不影响整批。
//
// 并发不会改变任何一条数据的产出方式：每条 Item 仍由它自己的 goroutine 独占写入，
// 处理顺序与入参切片顺序无关（下游按切片下标取用，不依赖处理顺序）。
//
// ctx 取消（例如 Ctrl-C）后不再对剩余条目发起接口调用；在飞的请求随 ctx 一起取消。
// 这里不做"留白"以外的补偿，调用方（Run）会照常落库已处理完的部分。
func (e *Enricher) EnrichAll(ctx context.Context, items []domain.Item) {
	if len(items) == 0 {
		return
	}
	if ctx.Err() != nil {
		log.Printf("富化被中断，剩余 %d 篇未处理: %v", len(items), ctx.Err())
		return
	}

	e.runItems(ctx, items)

	if err := ctx.Err(); err != nil {
		log.Printf("富化被中断: %v", err)
	}
}

// runItems 以最多 concurrency 个 goroutine 处理全部条目，直到全部完成或 ctx 取消。
//
// items[i] 被写入前会先等待它自己的两个前置任务（WaitGroup）。父 goroutine 只在所有
// 任务完成后才返回，因此调用方之后读 items 不会与这些写入竞争。
func (e *Enricher) runItems(ctx context.Context, items []domain.Item) {
	var (
		wg    sync.WaitGroup
		slots = semaphore.New(e.concurrency)
	)

	for i := range items {
		wg.Add(1)
		go func(item *domain.Item) {
			defer wg.Done()
			if err := slots.Acquire(ctx); err != nil {
				return // ctx 已取消，不再对这条发起接口调用
			}
			defer slots.Release()
			e.enrich(ctx, item)
		}(&items[i])
	}

	wg.Wait()
}

// enrich 就地为单条 Item 补齐 summary 与中文字段。
//
// "生成介绍"与"翻译"并发执行：两者互不依赖，且分别受各自客户端的闸门限流。
func (e *Enricher) enrich(ctx context.Context, item *domain.Item) {
	// 语种判定只读原文，先算出来，避免在并发的翻译分支里再做一次判断。
	isChinese := lang.IsMostlyChinese(sourceText(*item), ChineseRatioThreshold)

	var (
		wg                  sync.WaitGroup
		summary             string
		titleZh, abstractZh string
		translateErr        error
		needTranslation     = !isChinese && e.translator != nil
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		summary = e.buildSummary(ctx, *item)
	}()

	if needTranslation {
		wg.Add(1)
		go func() {
			defer wg.Done()
			titleZh, abstractZh, translateErr = e.translator.TranslateItem(ctx, item.Title, item.Abstract)
		}()
	}

	wg.Wait()

	item.Summary = summary

	// 原文已经是中文时无需翻译，只标记语种。
	if isChinese {
		item.IsChinese = true
		return
	}
	item.IsChinese = false

	if !needTranslation {
		return
	}
	if translateErr != nil {
		log.Printf("翻译失败（保留空的中文字段）: title=%q, err=%v", item.Title, translateErr)
		return
	}
	item.TitleChinese = titleZh
	item.AbstractChinese = abstractZh
}

// buildSummary 生成文献介绍：优先调用 LLM，失败或未配置时回退为摘要原文/标题。
func (e *Enricher) buildSummary(ctx context.Context, item domain.Item) string {
	fallback := fallbackSummary(item)

	// 没有摘要时没有可供概括的素材，直接用标题，省掉一次 LLM 调用。
	if e.summarizer == nil || strings.TrimSpace(item.Abstract) == "" {
		return fallback
	}

	summary, err := e.summarizer.GenerateSummary(ctx, item.Title, item.Abstract)
	if err != nil {
		log.Printf("生成文献介绍失败（回退为摘要原文）: title=%q, err=%v", item.Title, err)
		return fallback
	}
	if strings.TrimSpace(summary) == "" {
		return fallback
	}
	return strings.TrimSpace(summary)
}

// fallbackSummary 兜底文本：优先摘要原文，其次标题。
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
