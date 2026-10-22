// Package maintain 提供数据库维护操作，供命令行直接调用：
//
//	stats        查看总共有多少条 item 记录（MySQL 行数 + Milvus 向量数）
//	dates        查看记录覆盖了哪些日期（按日期倒序）
//	delete-date  删除指定日期的全部记录（MySQL 硬删除 + Milvus 按主键删除）
//	purge        清空全部 item 记录（MySQL 硬删除 + Milvus 集合重建）
//
// 数据本身（数量、日期表）走 stdout，诊断/提示信息走 log（stderr）。
package maintain

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"searcher/store"
)

// Op 是一次维护操作的类型。
type Op string

const (
	OpStats      Op = "stats"       // 查看记录总数
	OpDates      Op = "dates"       // 查看记录覆盖的日期
	OpDeleteDate Op = "delete-date" // 删除指定日期的记录
	OpPurge      Op = "purge"       // 清空全部记录
)

// Ops 返回所有支持的操作，用于错误提示与帮助文本。
func Ops() []Op { return []Op{OpStats, OpDates, OpDeleteDate, OpPurge} }

// Repository 是维护操作需要的存储能力，由 store.Store 实现。
type Repository interface {
	Collection() string
	CountItems(ctx context.Context) (store.ItemCount, error)
	CountItemsByDate(ctx context.Context, publishDate string) (int64, error)
	ListItemDates(ctx context.Context) ([]store.DateCount, error)
	VectorCount(ctx context.Context) (int64, error)
	DeleteByDate(ctx context.Context, publishDate string) (store.DateDeleteResult, error)
	PurgeAll(ctx context.Context) (store.PurgeResult, error)
}

// Request 描述一次维护请求。
type Request struct {
	Op Op
	// Date 只在 OpDeleteDate 时有意义：要删除的 publish_date（YYYY-MM-DD）。
	Date string
	// Confirmed 只在 OpPurge / OpDeleteDate 时有意义：
	// 为假时仅报告将要删除的规模并拒绝执行。
	Confirmed bool
}

// Runner 执行维护操作。
type Runner struct {
	repo Repository
}

// NewRunner 构造 Runner。
func NewRunner(repo Repository) (*Runner, error) {
	if repo == nil {
		return nil, fmt.Errorf("maintain: 必须提供 Repository")
	}
	return &Runner{repo: repo}, nil
}

// Run 执行 req 指定的操作。
func (r *Runner) Run(ctx context.Context, req Request) error {
	switch req.Op {
	case OpStats:
		return r.stats(ctx)
	case OpDates:
		return r.dates(ctx)
	case OpDeleteDate:
		return r.deleteDate(ctx, req.Date, req.Confirmed)
	case OpPurge:
		return r.purge(ctx, req.Confirmed)
	default:
		return fmt.Errorf("未知操作 %q，可选：%s / %s / %s / %s", req.Op, OpStats, OpDates, OpDeleteDate, OpPurge)
	}
}

// stats 查看记录总数：MySQL 行数与 Milvus 向量数一起报告，并在两者不一致时提示。
func (r *Runner) stats(ctx context.Context) error {
	count, err := r.repo.CountItems(ctx)
	if err != nil {
		return err
	}
	vectors, err := r.repo.VectorCount(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("MySQL items 表：%d 条记录（其中已软删除 %d 条）\n", count.Total, count.Deleted)
	fmt.Printf("Milvus 集合 %s：%d 条向量\n", r.repo.Collection(), vectors)

	switch {
	case count.Total > vectors:
		log.Printf("提示：MySQL 比 Milvus 多 %d 条，这部分记录没有向量，检索不到（可重跑抓取补齐，但标识会被去重挡住）", count.Total-vectors)
	case vectors > count.Total:
		log.Printf("提示：Milvus 比 MySQL 多 %d 条向量，属于孤儿向量，检索命中后会因查不到记录被跳过", vectors-count.Total)
	}
	return nil
}

// dates 查看记录覆盖的日期分布。
func (r *Runner) dates(ctx context.Context) error {
	rows, err := r.repo.ListItemDates(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("没有任何 item 记录")
		return nil
	}

	var total, deleted int64
	fmt.Printf("%-14s %10s %10s\n", "publish_date", "items", "deleted")
	for _, row := range rows {
		total += row.Total
		deleted += row.Deleted
		date := row.PublishDate
		if date == "" {
			date = "(空)"
		}
		fmt.Printf("%-14s %10d %10d\n", date, row.Total, row.Deleted)
	}
	log.Printf("共 %d 个日期，%d 条记录（其中已软删除 %d 条）", len(rows), total, deleted)
	return nil
}

// deleteDate 删除指定日期的全部记录；未确认时只报告规模，不执行任何删除。
//
// 典型用法：某天的数据抓坏了（例如接口限流导致大量 summary 回退成摘要原文），
// 先删掉这一天，再用 -from/-to 重新收集。
func (r *Runner) deleteDate(ctx context.Context, date string, confirmed bool) error {
	date = strings.TrimSpace(date)
	if date == "" {
		return fmt.Errorf("delete-date 必须提供 -date，例如 -date 2024-06-20")
	}
	if !validDate(date) {
		return fmt.Errorf("日期格式错误，应为 YYYY-MM-DD: %q", date)
	}

	count, err := r.repo.CountItemsByDate(ctx, date)
	if err != nil {
		return err
	}
	if count == 0 {
		fmt.Printf("%s 没有记录，无需删除\n", date)
		return nil
	}

	if !confirmed {
		return fmt.Errorf(
			"将永久删除 %s 的 %d 条记录（含其中已软删除的行）及其在 Milvus 集合 %s 中的向量，无法恢复；确认执行请加 -yes",
			date, count, r.repo.Collection(),
		)
	}

	log.Printf("开始删除 %s 的记录（MySQL %d 条 + Milvus 对应向量）...", date, count)
	result, err := r.repo.DeleteByDate(ctx, date)
	if err != nil {
		return fmt.Errorf("删除失败（可能只完成了一部分，请用 -op stats 与 -op dates 复核）: %w", err)
	}
	log.Printf("删除完成：%s 从 MySQL 删除 %d 条，从 Milvus 删除 %d 条向量",
		result.PublishDate, result.MySQLDeleted, result.MilvusDeleted)
	return nil
}

// validDate 校验 YYYY-MM-DD 格式，并且必须是真实存在的日期（拒绝 2024-02-31 这类）。
// 单独校验是因为 publish_date 是字符串列，拼错的日期只会"删除 0 条"而不报错，
// 看起来像成功——容易让人误以为已经删掉了。
func validDate(date string) bool {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	return parsed.Format("2006-01-02") == date
}

// purge 清空全部记录；未确认时只报告规模，不执行任何删除。
func (r *Runner) purge(ctx context.Context, confirmed bool) error {
	count, err := r.repo.CountItems(ctx)
	if err != nil {
		return err
	}
	vectors, err := r.repo.VectorCount(ctx)
	if err != nil {
		return err
	}

	if !confirmed {
		return fmt.Errorf(
			"purge 会永久删除 MySQL 的 %d 条记录与 Milvus 集合 %s 的 %d 条向量，无法恢复；确认执行请加 -yes",
			count.Total, r.repo.Collection(), vectors,
		)
	}

	log.Printf("开始清空：MySQL %d 条记录、Milvus 集合 %s 的 %d 条向量...", count.Total, r.repo.Collection(), vectors)
	result, err := r.repo.PurgeAll(ctx)
	if err != nil {
		return fmt.Errorf("清空失败（可能只完成了一部分，请用 -op stats 复核）: %w", err)
	}
	log.Printf("清空完成：MySQL 删除 %d 条；Milvus 集合 %s 已重建，清空前有 %d 条向量",
		result.MySQLDeleted, result.Collection, result.MilvusVectors)
	return nil
}
