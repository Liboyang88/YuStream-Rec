// Package ingest 编排一次完整的抓取入库流程：
//
//	抓取 arXiv → 映射为 Item → 按唯一标识去重 → 富化（文献介绍 + 翻译）
//	→ 向量化 → 写入 MySQL + Milvus
//
// 去重先于富化：摘要生成与翻译都要调用外部付费接口，库里已存在的论文没有必要
// 再调用一次。
package ingest

import (
	"context"
	"fmt"
	"log"
	"time"

	"searcher/arxiv"
	"searcher/domain"
)

// Source 提供指定日期提交的论文。
type Source interface {
	FetchSubmitted(ctx context.Context, date time.Time, maxResults int) ([]arxiv.Paper, error)
}

// Vectorizer 把文本编码为向量。
type Vectorizer interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model / Dim 仅用于日志输出。
	Model() string
	Dim() int
}

// Repository 提供去重查询与落库能力。
type Repository interface {
	// ExistingIdentifiers 返回这些唯一标识中已存在于库里的部分。
	// 传入的标识已由 domain.Item.Identifier 裁剪过空白，实现只需按值比较。
	ExistingIdentifiers(ctx context.Context, identifiers []string) (map[string]struct{}, error)
	// SaveItems 写入 MySQL 与 Milvus，items 与 vectors 必须一一对应。
	SaveItems(ctx context.Context, items []domain.Item, vectors [][]float32) error
}

// Request 是一次抓取任务的参数。
type Request struct {
	// Date 指 arXiv submittedDate 的当天（美东时间 ET）。
	Date time.Time
	// Max <= 0 表示抓取当天全部论文（自动分页）。
	Max int
	// DryRun 为真时只打印映射结果：不查库、不调用 LLM、不写库。
	DryRun bool
}

// Deps 是 Runner 的依赖集合。Summarizer / Translator / Vectorizer / Repository
// 允许为空，表示对应能力未配置；Run 会按需跳过或报错。
type Deps struct {
	Source     Source
	Summarizer Summarizer
	Translator Translator
	Vectorizer Vectorizer
	Repository Repository
	// EnrichConcurrency 是富化阶段同时处理的论文数；<= 0 时用 DefaultConcurrency。
	// 各接口自己的并发上限由客户端闸门控制，与这里无关。
	EnrichConcurrency int
}

// Runner 串联各个依赖，执行一次抓取入库。
type Runner struct {
	source     Source
	enricher   *Enricher
	vectorizer Vectorizer
	repo       Repository
}

// NewRunner 构造 Runner；只做参数校验，不建立任何连接。
func NewRunner(deps Deps) (*Runner, error) {
	if deps.Source == nil {
		return nil, fmt.Errorf("ingest: 必须提供 Source")
	}
	return &Runner{
		source:     deps.Source,
		enricher:   NewEnricher(deps.Summarizer, deps.Translator, WithConcurrency(deps.EnrichConcurrency)),
		vectorizer: deps.Vectorizer,
		repo:       deps.Repository,
	}, nil
}

// Run 执行一次抓取入库。
func (r *Runner) Run(ctx context.Context, req Request) error {
	date := FormatDate(req.Date)
	if req.Max > 0 {
		log.Printf("抓取 %s 的 arXiv 论文（最多 %d 篇）...", date, req.Max)
	} else {
		log.Printf("抓取 %s 的 arXiv 当天全部论文（自动分页）...", date)
	}

	papers, err := r.source.FetchSubmitted(ctx, req.Date, req.Max)
	if err != nil {
		return fmt.Errorf("抓取 arXiv 论文失败: %w", err)
	}
	if len(papers) == 0 {
		log.Printf("%s 没有抓到论文，退出", date)
		return nil
	}
	log.Printf("抓到 %d 篇论文", len(papers))

	items := ToItems(papers)

	if req.DryRun {
		printItems(items)
		log.Printf("dry-run 结束，共 %d 篇，未调用 LLM、未写入数据库", len(items))
		return nil
	}

	// 先确认写入所需的依赖齐备，避免白白调用 LLM 之后才发现写不了。
	if r.repo == nil {
		return fmt.Errorf("未配置存储，无法去重与写入")
	}
	if r.vectorizer == nil {
		return fmt.Errorf("未配置向量化服务，无法写入")
	}

	kept, err := r.filterNew(ctx, items)
	if err != nil {
		return err
	}
	if len(kept) == 0 {
		log.Print("全部已存在，无需写入")
		return nil
	}

	log.Printf("生成文献介绍与中文翻译（%d 篇，并发 %d）...", len(kept), r.enricher.Concurrency())
	r.enricher.EnrichAll(ctx, kept)

	texts := Summaries(kept)
	log.Printf("生成 %d 条 embedding（模型=%s, 维度=%d）...", len(texts), r.vectorizer.Model(), r.vectorizer.Dim())
	vectors, err := r.vectorizer.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embedding 失败: %w", err)
	}

	if err := r.repo.SaveItems(ctx, kept, vectors); err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}
	log.Printf("成功写入 %d 篇论文到 MySQL 和 Milvus", len(kept))
	return nil
}

// filterNew 按唯一标识去重：先问库里有哪些标识，再结合批内重复过滤。
func (r *Runner) filterNew(ctx context.Context, items []domain.Item) ([]domain.Item, error) {
	existing, err := r.repo.ExistingIdentifiers(ctx, Identifiers(items))
	if err != nil {
		return nil, fmt.Errorf("去重查询失败: %w", err)
	}

	kept, stats := KeepNew(items, existing)
	log.Printf("按标识去重：共 %d 篇，批内重复 %d 篇，库中已存在 %d 篇，待写入 %d 篇（无标识 %d 篇）",
		stats.Fetched, stats.DuplicateInBatch, stats.AlreadyStored, stats.Kept, stats.WithoutIdentifier)
	return kept, nil
}

// Summaries 返回各条目的 summary 文本，顺序与 items 一致（embedding 的输入）。
func Summaries(items []domain.Item) []string {
	texts := make([]string, 0, len(items))
	for _, it := range items {
		texts = append(texts, it.Summary)
	}
	return texts
}

// FormatDate 返回日志中使用的日期格式。
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// printItems 打印 dry-run 结果。
func printItems(items []domain.Item) {
	for i, it := range items {
		fmt.Printf("[%d] %s | %s | 作者:%d人 | 分类:%v | 标识:%s\n",
			i+1, it.Title, it.PublishDate, len(it.Authors), it.Fields, it.Identifier())
	}
}
