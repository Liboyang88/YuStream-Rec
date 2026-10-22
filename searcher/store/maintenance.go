package store

import (
	"context"
	"fmt"
	"log"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"searcher/domain"
	"searcher/storefields"
)

const (
	// deleteBatchSize 是单次 Milvus 按主键删除的最大主键数（与 queryBatchSize 同量级）。
	deleteBatchSize = 500
)

// ItemCount 是 items 表的记录数快照。
type ItemCount struct {
	Total   int64 // 全部行，含已软删除的行
	Deleted int64 // 其中已软删除的行
}

// DateCount 是某个 publish_date 下的记录数。
type DateCount struct {
	PublishDate string
	Total       int64
	Deleted     int64
}

// PurgeResult 描述一次清空操作的结果。
type PurgeResult struct {
	MySQLDeleted  int64  // 从 items 表删掉的行数（含软删除行）
	MilvusVectors int64  // 清空前集合里的向量数
	Collection    string // 被重建的 Milvus 集合名
}

// DateDeleteResult 描述一次"按日期删除"的结果。
type DateDeleteResult struct {
	PublishDate   string // 被删除的日期（YYYY-MM-DD）
	MySQLDeleted  int64  // 从 items 表删掉的行数（含软删除行）
	MilvusDeleted int64  // 从 Milvus 删掉的向量数
}

// Collection 返回当前使用的 Milvus 集合名。
func (s *Store) Collection() string { return s.collection }

// CountItems 统计 items 表记录数。统计口径包含软删除行，与去重口径
// （ExistingIdentifiers 用 Unscoped）保持一致，避免"显示 0 条但仍抓不进来"的困惑。
func (s *Store) CountItems(ctx context.Context) (ItemCount, error) {
	var count ItemCount
	err := s.db.WithContext(ctx).
		Unscoped().
		Model(&domain.Item{}).
		Select("COUNT(*) AS total, COUNT(deleted_at) AS deleted").
		Scan(&count).Error
	if err != nil {
		return ItemCount{}, fmt.Errorf("统计 items 记录数失败: %w", err)
	}
	return count, nil
}

// ListItemDates 返回记录覆盖的日期及其条数，按日期倒序（最新在前）。
// 同样包含软删除行，Deleted 列给出其中已软删除的条数。
func (s *Store) ListItemDates(ctx context.Context) ([]DateCount, error) {
	var rows []DateCount
	// 用 COUNT(deleted_at)（只统计非 NULL）而不是 SUM(deleted_at IS NOT NULL)：
	// SUM 在 MySQL 返回 DECIMAL，扫进 int64 不可靠。
	err := s.db.WithContext(ctx).
		Unscoped().
		Model(&domain.Item{}).
		Select("publish_date, COUNT(*) AS total, COUNT(deleted_at) AS deleted").
		Group("publish_date").
		Order("publish_date DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("统计日期分布失败: %w", err)
	}
	return rows, nil
}

// VectorCount 返回 Milvus 集合中的向量数；集合不存在时返回 0。
//
// 计数前会先 Flush 一次：仍在增长段、尚未落盘的向量不参与 count(*) 聚合，
// 刚写入就读会得到 0（集合统计同样滞后）。维护操作要的是准确数字，所以主动封存
// 一次增长段——Flush 是幂等的，不会改动已有数据。
func (s *Store) VectorCount(ctx context.Context) (int64, error) {
	exist, err := s.milvus.HasCollection(ctx, s.collection)
	if err != nil {
		return 0, fmt.Errorf("检查 Milvus 集合失败: %w", err)
	}
	if !exist {
		return 0, nil
	}

	if err := s.milvus.Flush(ctx, s.collection, false); err != nil {
		return 0, fmt.Errorf("落盘 Milvus 数据失败: %w", err)
	}

	res, err := s.milvus.Query(ctx, s.collection, []string{}, "", []string{"count(*)"})
	if err != nil {
		return 0, fmt.Errorf("统计 Milvus 向量数失败: %w", err)
	}
	if len(res) == 0 {
		return 0, nil
	}

	// 正常情况下列名就是 "count(*)"；若该版本未回填别名，取第一列兜底。
	col := res.GetColumn("count(*)")
	if col == nil {
		col = res[0]
	}
	countCol, ok := col.(*entity.ColumnInt64)
	if !ok {
		return 0, fmt.Errorf("Milvus count(*) 返回了非整型列: %T", col)
	}
	data := countCol.Data()
	if len(data) == 0 {
		return 0, nil
	}
	return data[0], nil
}

// PurgeAll 清空 items 表与 Milvus 集合里的全部记录。
//
// MySQL 侧是硬删除（Unscoped）：软删除的行在去重时仍被视为"已存在"，只做软删除
// 会导致清空后同一篇论文再也抓不回来。
//
// Milvus 侧直接删掉集合并按同一 schema 重建（含索引与 load），这样连历史遗留的
// 孤儿向量也一并清掉，清空后依然是"空但可用"的状态（后端无需重启即可继续检索）。
//
// 自增 ID 不重置：重置会让后续 item 复用旧 ID，而后端 Redis 里可能还留着按 item id
// 记录的旧数据，复用 ID 会把新旧数据混在一起。
//
// 执行顺序是先 MySQL 后 Milvus：中途失败时留下的是孤儿向量（检索查不到记录会被跳过，
// 无害），而不是"有行但检索不到"的记录。
func (s *Store) PurgeAll(ctx context.Context) (PurgeResult, error) {
	result := PurgeResult{Collection: s.collection}

	vectors, err := s.VectorCount(ctx)
	if err != nil {
		return PurgeResult{}, err
	}
	result.MilvusVectors = vectors

	// Where("1 = 1") 是为了满足 gorm 的全局删除保护（不允许无条件 Delete）。
	deleted := s.db.WithContext(ctx).Unscoped().Where("1 = 1").Delete(&domain.Item{})
	if deleted.Error != nil {
		return PurgeResult{}, fmt.Errorf("清空 items 表失败: %w", deleted.Error)
	}
	result.MySQLDeleted = deleted.RowsAffected

	if err := s.dropCollection(ctx); err != nil {
		return result, err
	}
	if err := s.ensureCollection(ctx); err != nil {
		return result, fmt.Errorf("重建 Milvus 集合失败: %w", err)
	}
	return result, nil
}

// CountItemsByDate 统计某个 publish_date 下的记录数（含已软删除行）。
// 与删除口径一致：删除是按 publish_date 硬删除，统计也应包含软删除行，
// 否则会出现"报告 0 条、删除却动了数据"的错位。
func (s *Store) CountItemsByDate(ctx context.Context, publishDate string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Unscoped().
		Model(&domain.Item{}).
		Where("publish_date = ?", publishDate).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("统计 %s 的记录数失败: %w", publishDate, err)
	}
	return count, nil
}

// DeleteByDate 删除某个 publish_date 下的全部记录：MySQL 硬删除 + Milvus 按主键删除向量。
//
// 与 PurgeAll（重建集合）不同，这里只删这个日期对应的主键，集合与索引保持不变，
// 因此其他日期的向量与检索服务不受影响。
//
// 为什么是硬删除而不是软删除：软删除的行在去重（ExistingIdentifiers 用 Unscoped）
// 时仍被视为"已存在"，软删之后该日期的论文再也抓不回来；硬删除才能配合
// "删除 → 用 -from/-to 重新收集"的用法。
//
// 执行顺序是先 MySQL 后 Milvus，与 PurgeAll 一致：中途失败留下的是孤儿向量
// （检索命中后回表查不到记录会被跳过，无害），而不是"有行但检索不到"的记录。
// 注意：若 Milvus 这一步失败，MySQL 行已删掉，用 -op stats 会看到 Milvus 比 MySQL 多出的
// 孤儿向量；此时应重跑本操作（该日期已无 MySQL 记录，会直接返回 0 而不再清向量），
// 或者直接 -op purge 重建集合彻底清干净。
func (s *Store) DeleteByDate(ctx context.Context, publishDate string) (DateDeleteResult, error) {
	result := DateDeleteResult{PublishDate: publishDate}

	// 1. 先取出主键：Milvus 只存 id + vec，没有日期列，只能先按日期在 MySQL 里查 id。
	var ids []int64
	err := s.db.WithContext(ctx).
		Unscoped().
		Model(&domain.Item{}).
		Where("publish_date = ?", publishDate).
		Pluck("id", &ids).Error
	if err != nil {
		return DateDeleteResult{}, fmt.Errorf("查询 %s 的记录失败: %w", publishDate, err)
	}
	if len(ids) == 0 {
		return result, nil // 该日期没有记录，什么也不做
	}

	// 2. MySQL 硬删除（Unscoped 绕过软删除，真正删行）。
	deleted := s.db.WithContext(ctx).
		Unscoped().
		Where("publish_date = ?", publishDate).
		Delete(&domain.Item{})
	if deleted.Error != nil {
		return DateDeleteResult{}, fmt.Errorf("删除 %s 的 items 记录失败: %w", publishDate, deleted.Error)
	}
	result.MySQLDeleted = deleted.RowsAffected

	// 3. Milvus 按主键删除这批向量。
	if err := s.deleteVectorsByID(ctx, ids); err != nil {
		return result, fmt.Errorf("MySQL 已删除 %d 条，但 Milvus 向量删除失败: %w", result.MySQLDeleted, err)
	}
	result.MilvusDeleted = int64(len(ids))
	return result, nil
}

// deleteVectorsByID 按主键批量删除 Milvus 中的向量，返回删除的向量数。
// 集合不存在时视为无需删除（返回 0）：删除操作不该顺手把集合建出来。
func (s *Store) deleteVectorsByID(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	exist, err := s.milvus.HasCollection(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("检查 Milvus 集合失败: %w", err)
	}
	if !exist {
		// 集合都不存在，说明从来没有写入过向量，与"已删除"等价。
		log.Printf("提示：Milvus 集合 %s 不存在，没有向量需要删除", s.collection)
		return nil
	}

	// 分批发删除请求，避免单个表达式过长。
	for start := 0; start < len(ids); start += deleteBatchSize {
		end := min(start+deleteBatchSize, len(ids))
		batch := ids[start:end]

		// 主键列名传空串：SDK 内部会用实际的主键字段名（id）拼表达式，
		// 传错列名会被 SDK 直接拒绝。
		if err := s.milvus.DeleteByPks(ctx, s.collection, "", entity.NewColumnInt64("", batch)); err != nil {
			return fmt.Errorf("删除 Milvus 向量失败（第 %d-%d 个主键）: %w", start+1, end, err)
		}
	}
	return nil
}

// dropCollection 删除集合；集合不存在时视为成功。
func (s *Store) dropCollection(ctx context.Context) error {
	exist, err := s.milvus.HasCollection(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("检查 Milvus 集合失败: %w", err)
	}
	if !exist {
		return nil
	}
	if err := s.milvus.DropCollection(ctx, s.collection); err != nil {
		return fmt.Errorf("删除 Milvus 集合失败: %w", err)
	}
	return nil
}

// ---- 按日期重新生成（regen-translate / regen-summary / regen-embedding）----

// ListItemsByDate 取出某个 publish_date 下的全部记录，按主键升序。
//
// 用于"按日期重新生成"：拿到原始 title/abstract 才能重算 summary 与翻译。
// 用 Unscoped 与删除口径保持一致（delete-date 也删软删除行），
// 但重新生成只改字段、不涉及去重，因此调用方拿到 id 后按 id 精确回写。
func (s *Store) ListItemsByDate(ctx context.Context, publishDate string) ([]domain.Item, error) {
	var items []domain.Item
	err := s.db.WithContext(ctx).
		Unscoped().
		Where("publish_date = ?", publishDate).
		Order("id ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询 %s 的记录失败: %w", publishDate, err)
	}
	return items, nil
}

// UpdateTranslations 按主键批量回写中文字段。
//
// 只更新这三列：重新生成翻译不该顺手改动 title/abstract/summary 等别的字段
// （用 Save 整行覆盖会把并发期间别人改过的内容一起写回去）。
func (s *Store) UpdateTranslations(ctx context.Context, updates []storefields.TranslationUpdate) (int64, error) {
	return s.updateColumns(ctx, len(updates), func(i int) (uint, map[string]any) {
		u := updates[i]
		return u.ID, map[string]any{
			"title_chinese":    u.TitleChinese,
			"abstract_chinese": u.AbstractChinese,
			"is_chinese":       u.IsChinese,
		}
	})
}

// UpdateSummaries 按主键批量回写 summary 列。
func (s *Store) UpdateSummaries(ctx context.Context, updates []storefields.SummaryUpdate) (int64, error) {
	return s.updateColumns(ctx, len(updates), func(i int) (uint, map[string]any) {
		return updates[i].ID, map[string]any{"summary": updates[i].Summary}
	})
}

// updateColumns 按主键逐条更新指定列，返回成功更新的行数。
//
// 逐条而不是拼一条大 SQL：重新生成的条数通常在几百到几千，单条 UPDATE 走主键索引
// 很快，且中途失败时已更新的部分是有意义的进度（不必整批重来）。
// 单条失败只记日志并跳过，不影响其余条目——与富化阶段"单条失败只降级该条"一致。
func (s *Store) updateColumns(ctx context.Context, n int, build func(i int) (uint, map[string]any)) (int64, error) {
	var updated int64
	var failed int
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return updated, fmt.Errorf("更新被中断（已更新 %d/%d 条）: %w", updated, n, err)
		}
		id, cols := build(i)
		res := s.db.WithContext(ctx).
			Model(&domain.Item{}).
			Where("id = ?", id).
			Updates(cols)
		if res.Error != nil {
			failed++
			log.Printf("更新记录 id=%d 失败（跳过，可重跑本操作补齐）: %v", id, res.Error)
			continue
		}
		updated += res.RowsAffected
	}
	if failed > 0 {
		log.Printf("提示：%d/%d 条更新失败，已成功 %d 条；重跑同一操作会把它们补齐", failed, n, updated)
	}
	return updated, nil
}

// ReplaceVectors 用新向量替换指定主键在 Milvus 中的旧向量（先删后插）。
//
// 为什么不是 Upsert：集合的 id 是显式主键（AutoID=false），先删再插的语义在各版本
// Milvus 上最可预期，也不会留下旧向量与新向量并存的中间状态。
//
// 顺序是先删后插：两次都失败时结果是"没有向量"（检索不到，但可重跑补齐），
// 好过新旧向量并存——同一个 id 出现两条向量会让检索结果不稳定。
//
// 集合不存在时先按 schema 创建并加载（换 embedding 模型后重跑也走这条路）。
// 返回成功写入的向量数；单条维度不符会整批拒绝（写进去只会让检索报错）。
func (s *Store) ReplaceVectors(ctx context.Context, updates []storefields.VectorUpdate) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	for i, u := range updates {
		if len(u.Vec) != s.vecDim {
			return 0, fmt.Errorf("第 %d 条向量维度不符: 得到 %d, 期望 %d（不写入，避免污染集合）",
				i+1, len(u.Vec), s.vecDim)
		}
	}

	if err := s.ensureCollection(ctx); err != nil {
		return 0, fmt.Errorf("准备 Milvus 集合失败: %w", err)
	}

	// 1. 先删旧向量。集合刚创建时里面没有数据，删除是无害的空操作。
	ids := make([]int64, 0, len(updates))
	for _, u := range updates {
		ids = append(ids, int64(u.ID))
	}
	if err := s.deleteVectorsByID(ctx, ids); err != nil {
		return 0, fmt.Errorf("删除旧向量失败: %w", err)
	}

	// 2. 再插新向量，分批避免单次请求过大。
	var written int64
	for start := 0; start < len(updates); start += writeBatchSize {
		end := min(start+writeBatchSize, len(updates))
		batch := updates[start:end]

		batchIDs := make([]int64, 0, len(batch))
		batchVecs := make([][]float32, 0, len(batch))
		for _, u := range batch {
			batchIDs = append(batchIDs, int64(u.ID))
			batchVecs = append(batchVecs, u.Vec)
		}

		if _, err := s.milvus.Insert(ctx, s.collection, "",
			entity.NewColumnInt64(idField, batchIDs),
			entity.NewColumnFloatVector(vecField, s.vecDim, batchVecs),
		); err != nil {
			return written, fmt.Errorf("写入新向量失败（第 %d-%d 条，共 %d 条）：已删除旧向量，请重跑本操作补齐: %w",
				start+1, end, len(updates), err)
		}
		written += int64(len(batch))
	}
	return written, nil
}
