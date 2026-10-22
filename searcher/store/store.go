// Package store 负责把 Item 写入 MySQL 与 Milvus，并提供"标识是否已存在"的查询。
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"searcher/domain"
)

const (
	// queryBatchSize 是单条 SQL 中 IN 列表的元素上限，避免 SQL 过长。
	queryBatchSize = 500

	// defaultConnectTimeout 是连接 Milvus 的超时时间。
	// SDK 默认带 grpc.WithBlock() 建连：Milvus 没启动时会一直阻塞到 ctx 结束，
	// 最后只报一句 "context canceled"，既慢又难定位，因此这里给一个明确上限。
	defaultConnectTimeout = 15 * time.Second

	// writeBatchSize 是单批写入 MySQL / Milvus 的条数上限：
	// 一条 INSERT 塞入上千篇带长摘要的论文容易触发 max_allowed_packet 限制，
	// 拆批写入更稳。
	writeBatchSize = 200

	idField  = "id"
	vecField = "vec"
)

// Options 是连接存储所需的参数。
type Options struct {
	DSN           string // MySQL DSN，与后端共用同一个库
	MilvusAddress string
	Collection    string
	VecDim        int

	// SkipCollectionInit 为真时不在连接阶段创建 Milvus 集合。
	// 只读的维护操作（查看记录数/日期）用它避免副作用：集合不存在时保持不存在，
	// 而不是"查一下数量却顺手建了个集合"。
	SkipCollectionInit bool

	// ConnectTimeout 是连接 Milvus 的超时时间，<=0 时用 defaultConnectTimeout。
	ConnectTimeout time.Duration
}

// Store 聚合 MySQL 与 Milvus 两个后端。
type Store struct {
	db         *gorm.DB
	milvus     client.Client
	collection string
	vecDim     int
}

// Open 连接 MySQL 与 Milvus，并确保 items 表已迁移、Milvus 集合可用。
// 任一步失败都会关闭已经建立的连接，不留泄漏。
func Open(ctx context.Context, opts Options) (*Store, error) {
	ok := false
	var db *gorm.DB
	var cli client.Client
	defer func() {
		if ok {
			return
		}
		if db != nil {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if cli != nil {
			_ = cli.Close()
		}
	}()

	var err error
	db, err = gorm.Open(mysql.Open(opts.DSN), &gorm.Config{})
	if err != nil {
		db = nil
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	if err := db.WithContext(ctx).AutoMigrate(&domain.Item{}); err != nil {
		return nil, fmt.Errorf("迁移 items 表失败: %w", err)
	}

	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	connectCtx, cancelConnect := context.WithTimeout(ctx, timeout)
	defer cancelConnect()

	cli, err = client.NewClient(connectCtx, client.Config{Address: opts.MilvusAddress})
	if err != nil {
		cli = nil
		// 区分"被中断"、"连不上（超时）"和"其他错误"，避免只留一句 context canceled。
		switch {
		case ctx.Err() != nil:
			return nil, fmt.Errorf("连接 Milvus 被中断（%s）: %w", opts.MilvusAddress, ctx.Err())
		case connectCtx.Err() != nil:
			return nil, fmt.Errorf("连接 Milvus 超时（%s 超过 %s）: 请确认 Milvus 已启动、MILVUS_ADDRESS 正确",
				opts.MilvusAddress, timeout)
		default:
			return nil, fmt.Errorf("连接 Milvus 失败（%s）: %w", opts.MilvusAddress, err)
		}
	}

	s := &Store{
		db:         db,
		milvus:     cli,
		collection: opts.Collection,
		vecDim:     opts.VecDim,
	}
	if !opts.SkipCollectionInit {
		if err := s.ensureCollection(ctx); err != nil {
			return nil, fmt.Errorf("初始化 Milvus 集合失败: %w", err)
		}
	}

	ok = true
	return s, nil
}

// ExistingIdentifiers 返回 identifiers 中已存在于 items 表的那些标识。
//
// 去重口径：`items.url` 即唯一标识（arXiv ID 写入该列，见 domain.Item.Identifier）。
// 查询默认包含被软删除的行（Unscoped）：若一篇论文曾入库后被删除，再次抓取时
// 也应视为已存在，否则会插入一行 url 完全相同的重复数据。
func (s *Store) ExistingIdentifiers(ctx context.Context, identifiers []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{})

	uniq := uniqueNonEmpty(identifiers)
	for start := 0; start < len(uniq); start += queryBatchSize {
		end := min(start+queryBatchSize, len(uniq))

		var found []string
		err := s.db.WithContext(ctx).
			Unscoped().
			Model(&domain.Item{}).
			Where("url IN ?", uniq[start:end]).
			Pluck("url", &found).Error
		if err != nil {
			return nil, fmt.Errorf("查询已存在的标识失败: %w", err)
		}
		for _, id := range found {
			existing[id] = struct{}{}
		}
	}
	return existing, nil
}

// SaveItems 批量写入：先写 MySQL（回填自增 ID），再把 (id, vec) 写入 Milvus。
//
// MySQL 写入放在一个事务里，Milvus 写入失败时回滚，避免留下"MySQL 有行、Milvus 无向量"
// 的不可见数据：检索走 Milvus，这类行永远搜不到；而它的标识已存在，又会被去重挡住，
// 于是永远补不上向量。
//
// 取舍：回滚只覆盖 MySQL。失败前已写入 Milvus 的向量会成为"孤儿向量"（对应行已回滚，
// 后端按 ID 回表 LoadMany 时查不到就跳过，只是占用一个候选位），重跑时会重新插入并生成
// 新 ID。宁可留下无害的孤儿向量，也不要留下搜不到的行。
//
// 注意：自增 ID 是写 MySQL 时回填到入参 items 上的（切片共享底层数组）。若中途回滚，
// 这些 ID 会留在调用方的切片里但库中并不存在，调用方失败后不应再使用它们。
func (s *Store) SaveItems(ctx context.Context, items []domain.Item, vectors [][]float32) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) != len(vectors) {
		return fmt.Errorf("items/vectors 长度不一致: %d vs %d", len(items), len(vectors))
	}

	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("开启 MySQL 事务失败: %w", tx.Error)
	}

	for start := 0; start < len(items); start += writeBatchSize {
		end := min(start+writeBatchSize, len(items))
		batchItems := items[start:end]
		batchVectors := vectors[start:end]

		// 1. MySQL 批量插入，自增 ID 会回填到 batchItems（与 items 共享底层数组）。
		if err := tx.Create(&batchItems).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("MySQL 插入失败（第 %d-%d 条）: %w", start+1, end, err)
		}

		// 2. 同一批写入 Milvus，保证 id 与向量一一对应。
		ids := make([]int64, 0, len(batchItems))
		vecCol := make([][]float32, 0, len(batchItems))
		for i := range batchItems {
			ids = append(ids, int64(batchItems[i].ID))
			vecCol = append(vecCol, batchVectors[i])
		}

		_, err := s.milvus.Insert(ctx, s.collection, "",
			entity.NewColumnInt64(idField, ids),
			entity.NewColumnFloatVector(vecField, s.vecDim, vecCol),
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("Milvus 插入失败（第 %d-%d 条）: %w", start+1, end, err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("提交 MySQL 事务失败: %w", err)
	}
	return nil
}

// Close 释放 MySQL 与 Milvus 连接，两个错误都会上报（后者不覆盖前者）。
func (s *Store) Close() error {
	var errs []error
	if sqlDB, dbErr := s.db.DB(); dbErr != nil {
		errs = append(errs, fmt.Errorf("获取 MySQL 连接失败: %w", dbErr))
	} else if err := sqlDB.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.milvus.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ensureCollection 保证集合可用：不存在则按后端的 schema 创建（id int64 主键 +
// vec 浮点向量，HNSW/L2）并建索引；无论新建还是复用，都确保集合已加载——
// 未加载的集合无法参与检索（否则写进去的向量永远搜不到）。
func (s *Store) ensureCollection(ctx context.Context) error {
	exist, err := s.milvus.HasCollection(ctx, s.collection)
	if err != nil {
		return err
	}
	if !exist {
		fields := []*entity.Field{
			{Name: idField, DataType: entity.FieldTypeInt64, PrimaryKey: true, AutoID: false},
			{Name: vecField, DataType: entity.FieldTypeFloatVector,
				TypeParams: map[string]string{entity.TypeParamDim: fmt.Sprintf("%d", s.vecDim)}},
		}
		schema := &entity.Schema{CollectionName: s.collection, Fields: fields}
		if err := s.milvus.CreateCollection(ctx, schema, 1); err != nil {
			return err
		}

		idx, err := entity.NewIndexHNSW(entity.L2, 16, 64) // 与后端一致：L2, M=16, efConstruction=64
		if err != nil {
			return err
		}
		if err := s.milvus.CreateIndex(ctx, s.collection, vecField, idx, false); err != nil {
			return err
		}
	}
	return s.milvus.LoadCollection(ctx, s.collection, false) // LoadCollection 幂等
}

// uniqueNonEmpty 去重并丢弃空串（空标识无法参与比较）。
func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
