package main

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Store struct {
	db         *gorm.DB
	milvus     client.Client
	collection string
	vecDim     int
}

func NewStore(env *Env) (*Store, error) {
	db, err := gorm.Open(mysql.Open(env.MysqlDsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	if err := db.AutoMigrate(&Item{}); err != nil {
		return nil, fmt.Errorf("迁移 items 表失败: %w", err)
	}

	ctx := context.Background()
	cli, err := client.NewClient(ctx, client.Config{Address: env.MilvusAddress})
	if err != nil {
		return nil, fmt.Errorf("连接 Milvus 失败: %w", err)
	}

	s := &Store{db: db, milvus: cli, collection: env.MilvusCollectionName, vecDim: env.VecDim}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, fmt.Errorf("初始化 Milvus 集合失败: %w", err)
	}
	return s, nil
}

// ensureCollection 集合不存在则创建，schema 与 dcpro 的 InitMilvus 一致（id int64 主键 + vec 浮点向量）。
func (s *Store) ensureCollection(ctx context.Context) error {
	exist, err := s.milvus.HasCollection(ctx, s.collection)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}
	fields := []*entity.Field{
		{Name: "id", DataType: entity.FieldTypeInt64, PrimaryKey: true, AutoID: false},
		{Name: "vec", DataType: entity.FieldTypeFloatVector,
			TypeParams: map[string]string{entity.TypeParamDim: fmt.Sprintf("%d", s.vecDim)}},
	}
	schema := &entity.Schema{CollectionName: s.collection, Fields: fields}
	if err := s.milvus.CreateCollection(ctx, schema, 1); err != nil {
		return err
	}
	idx, err := entity.NewIndexHNSW(entity.L2, 16, 64) // 与 dcpro 一致：L2, M=16, efConstruction=64
	if err != nil {
		return err
	}
	if err := s.milvus.CreateIndex(ctx, s.collection, "vec", idx, false); err != nil {
		return err
	}
	return s.milvus.LoadCollection(ctx, s.collection, false)
}

// SaveItems 写入 MySQL（回填自增 ID），再把 (id, vec) 批量写入 Milvus。
func (s *Store) SaveItems(items []Item, vectors [][]float32) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) != len(vectors) {
		return fmt.Errorf("items/vectors 长度不一致: %d vs %d", len(items), len(vectors))
	}

	// 1. MySQL 批量插入，回填自增 ID
	if err := s.db.Create(&items).Error; err != nil {
		return fmt.Errorf("MySQL 插入失败: %w", err)
	}

	// 2. Milvus 批量插入
	ids := make([]int64, 0, len(items))
	vecCol := make([][]float32, 0, len(items))
	for i := range items {
		ids = append(ids, int64(items[i].ID))
		vecCol = append(vecCol, vectors[i])
	}
	idColumn := entity.NewColumnInt64("id", ids)
	vecColumn := entity.NewColumnFloatVector("vec", s.vecDim, vecCol)
	if _, err := s.milvus.Insert(context.Background(), s.collection, "", idColumn, vecColumn); err != nil {
		return fmt.Errorf("Milvus 插入失败: %w", err)
	}
	return nil
}

// FilterExistingTitles 按标题去重：过滤掉 MySQL 中已存在的论文，避免重复抓取同一天。
func (s *Store) FilterExistingTitles(items []Item) ([]Item, error) {
	if len(items) == 0 {
		return items, nil
	}
	titles := make([]string, 0, len(items))
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	var existing []string
	if err := s.db.Model(&Item{}).Where("title IN ?", titles).Pluck("title", &existing).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(existing))
	for _, t := range existing {
		set[t] = true
	}
	out := items[:0]
	for _, it := range items {
		if !set[it.Title] {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Store) Close() error {
	if sqlDB, err := s.db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return s.milvus.Close()
}
