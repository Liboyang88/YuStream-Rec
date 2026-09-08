// arxiv-ingest：独立脚本，抓取指定日期 arXiv 论文，整理字段后写入 MySQL + Milvus。
// 与 dcpro 后端不共享代码，仅复用同一个 .env 配置文件。
//
// 用法：
//
//	go run . -date 2024-06-01                       # 抓取当天全部论文并入库（自动分页）
//	go run . -date 2024-06-01 -max 200 -dry-run     # 只抓前 200 篇打印，不写库
//	go run . -date 2024-06-01 -config ../backend/.env
//
// 注意：arXiv 的 submittedDate 按美东时间（ET）计，"当天"指 ET 的当天。
package main

import (
	"flag"
	"fmt"
	"log"
	"time"
)

func main() {
	dateStr := flag.String("date", "", "要抓取的日期，格式 YYYY-MM-DD（必填）")
	maxResults := flag.Int("max", 0, "最多抓取论文数；0 表示抓取当天全部（自动分页）")
	configPath := flag.String("config", "../backend/.env", "dcpro 的 .env 配置文件路径")
	dryRun := flag.Bool("dry-run", false, "只抓取并打印，不写入数据库")
	flag.Parse()

	if *dateStr == "" {
		log.Fatal("必须提供 -date，例如 -date 2024-06-01")
	}
	date, err := time.Parse("2006-01-02", *dateStr)
	if err != nil {
		log.Fatalf("日期格式错误，应为 YYYY-MM-DD: %v", err)
	}

	env, err := LoadEnv(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败（检查 -config 路径）: %v", err)
	}

	if *maxResults > 0 {
		log.Printf("抓取 %s 的 arXiv 论文（最多 %d 篇）...", date.Format("2006-01-02"), *maxResults)
	} else {
		log.Printf("抓取 %s 的 arXiv 当天全部论文（自动分页）...", date.Format("2006-01-02"))
	}
	papers, err := FetchArxivPapers(date, *maxResults)
	if err != nil {
		log.Fatalf("抓取失败: %v", err)
	}
	if len(papers) == 0 {
		log.Println("当天没有论文，退出")
		return
	}
	log.Printf("抓到 %d 篇论文", len(papers))

	// 映射到 Item，并准备待编码文本
	items := make([]Item, 0, len(papers))
	summaries := make([]string, 0, len(papers))
	for _, p := range papers {
		summary := p.Title
		if p.Abstract != "" {
			summary += "\n" + p.Abstract
		}
		pubDate := p.Published
		if len(pubDate) >= 10 {
			pubDate = pubDate[:10] // 只保留 YYYY-MM-DD
		}
		items = append(items, Item{
			Title:       p.Title,
			WebSource:   "arxiv",
			PublishDate: pubDate,
			Authors:     p.Authors,
			Abstract:    p.Abstract,
			Fields:      p.Categories,
			Keywords:    []string{}, // arXiv 无关键词字段，留空
			IsChinese:   false,
			Summary:     summary,
		})
		summaries = append(summaries, summary)
	}

	if *dryRun {
		for i, it := range items {
			fmt.Printf("[%d] %s | %s | 作者:%d人 | 分类:%v\n", i+1, it.Title, it.PublishDate, len(it.Authors), it.Fields)
		}
		log.Printf("dry-run 结束，未写入（共 %d 篇）", len(items))
		return
	}

	// 先连接存储做标题去重（避免重复抓同一天）
	store, err := NewStore(env)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer store.Close()

	items, err = store.FilterExistingTitles(items)
	if err != nil {
		log.Fatalf("去重查询失败: %v", err)
	}
	if len(items) == 0 {
		log.Println("全部已存在，无需写入")
		return
	}
	log.Printf("去重后待写入 %d 篇", len(items))

	// 重新对齐 summaries（只对去重后剩余的做 embedding）
	summaries = summaries[:0]
	for _, it := range items {
		summaries = append(summaries, it.Summary)
	}

	embedder := NewEmbedder(env.EmbeddingModelApiKey, env.EmbeddingModelBaseURL, env.VecDim)
	log.Printf("生成 %d 条 embedding（模型=%s, 维度=%d）...", len(summaries), embeddingModel, env.VecDim)
	vectors, err := embedder.Embed(summaries)
	if err != nil {
		log.Fatalf("embedding 失败: %v", err)
	}

	if err := store.SaveItems(items, vectors); err != nil {
		log.Fatalf("写入失败: %v", err)
	}
	log.Printf("成功写入 %d 篇论文到 MySQL 和 Milvus", len(items))
}
