package store

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"searcher/domain"
	"searcher/storefields"
)

// 这组用例会真实连接 MySQL 与 Milvus，默认跳过（未设置环境变量时 t.Skip）。
// 运行方式（务必指向一次性的库与集合，用例会重建/清空该集合）：
//
//	CREATE DATABASE litstream_searcher_it CHARACTER SET utf8mb4;
//	SEARCHER_IT_DSN='user:pass@tcp(127.0.0.1:3306)/litstream_searcher_it?charset=utf8mb4&parseTime=True&loc=Local' \
//	SEARCHER_IT_MILVUS_ADDRESS=127.0.0.1:19530 \
//	SEARCHER_IT_COLLECTION=litstream_searcher_it \
//	SEARCHER_IT_VEC_DIM=4 \
//	go test ./store/ -run Integration -v
func TestIntegrationSaveAndExistingIdentifiers(t *testing.T) {
	dsn := os.Getenv("SEARCHER_IT_DSN")
	milvusAddr := os.Getenv("SEARCHER_IT_MILVUS_ADDRESS")
	collection := os.Getenv("SEARCHER_IT_COLLECTION")
	dim := envInt(t, "SEARCHER_IT_VEC_DIM", 4)
	if dsn == "" || milvusAddr == "" || collection == "" {
		t.Skip("未设置 SEARCHER_IT_* 环境变量，跳过集成用例")
	}

	ctx := context.Background()

	cli, err := client.NewClient(ctx, client.Config{Address: milvusAddr})
	if err != nil {
		t.Fatalf("连接 Milvus: %v", err)
	}
	defer cli.Close()

	// 干净起点，并保证用例结束后不留下测试集合。
	if exist, err := cli.HasCollection(ctx, collection); err != nil {
		t.Fatalf("HasCollection: %v", err)
	} else if exist {
		if err := cli.DropCollection(ctx, collection); err != nil {
			t.Fatalf("DropCollection: %v", err)
		}
	}
	defer func() {
		if err := cli.DropCollection(ctx, collection); err != nil {
			t.Logf("清理测试集合失败（请手动删除 %s）: %v", collection, err)
		}
	}()

	st, err := Open(ctx, Options{DSN: dsn, MilvusAddress: milvusAddr, Collection: collection, VecDim: dim})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	// AutoMigrate 应包含 url 列：它是去重用的唯一标识。
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接 MySQL: %v", err)
	}
	if !db.Migrator().HasColumn(&domain.Item{}, "url") {
		t.Fatal("items 表缺少 url 列（去重依赖该列）")
	}

	// 用例起点必须干净：一次性库里可能残留上一次运行的记录。
	if err := db.Unscoped().Where("1 = 1").Delete(&domain.Item{}).Error; err != nil {
		t.Fatalf("清理残留数据: %v", err)
	}

	// ---- 1. 批量写入：回填自增 ID + 写向量 ----
	items := []domain.Item{
		newItem("http://arxiv.org/abs/2406.00001v1", "Paper A"),
		newItem("http://arxiv.org/abs/2406.00002v1", "Paper B"),
		newItem("http://arxiv.org/abs/2406.00003v1", "Paper C"),
	}
	vectors := [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}}

	if err := st.SaveItems(ctx, items, vectors); err != nil {
		t.Fatalf("SaveItems: %v", err)
	}
	for i := range items {
		if items[i].ID == 0 {
			t.Fatalf("第 %d 条未回填自增 ID: %+v", i, items[i])
		}
	}

	var mysqlCount int64
	if err := db.Model(&domain.Item{}).Where("url IN ?", []string{
		items[0].Url, items[1].Url, items[2].Url,
	}).Count(&mysqlCount).Error; err != nil {
		t.Fatalf("统计 MySQL 行数: %v", err)
	}
	if mysqlCount != 3 {
		t.Errorf("MySQL 中 url 匹配 %d 行, want 3", mysqlCount)
	}

	if err := cli.Flush(ctx, collection, false); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	res, err := cli.Query(ctx, collection, nil, "id > 0", []string{"id"})
	if err != nil {
		t.Fatalf("查询 Milvus 向量: %v", err)
	}
	if res.Len() != 3 {
		t.Errorf("Milvus 中 %d 条向量, want 3", res.Len())
	}

	// ---- 2. 按唯一标识查询 ----
	existing, err := st.ExistingIdentifiers(ctx, []string{
		items[0].Url,
		items[1].Url,
		"http://arxiv.org/abs/2406.99999v9", // 不存在
		"",                                  // 空标识应被忽略
	})
	if err != nil {
		t.Fatalf("ExistingIdentifiers: %v", err)
	}
	if len(existing) != 2 {
		t.Fatalf("命中 %d 个标识, want 2: %v", len(existing), existing)
	}
	for _, want := range []string{items[0].Url, items[1].Url} {
		if _, ok := existing[want]; !ok {
			t.Errorf("缺少命中标识 %q", want)
		}
	}
	if _, ok := existing["http://arxiv.org/abs/2406.99999v9"]; ok {
		t.Error("不存在的标识不应命中")
	}

	// ---- 3. 软删除的行同样算"已存在"，避免重复插入相同标识 ----
	if err := db.Delete(&domain.Item{}, items[0].ID).Error; err != nil {
		t.Fatalf("软删除: %v", err)
	}
	existing, err = st.ExistingIdentifiers(ctx, []string{items[0].Url})
	if err != nil {
		t.Fatalf("ExistingIdentifiers(软删除后): %v", err)
	}
	if _, ok := existing[items[0].Url]; !ok {
		t.Error("软删除的行仍应被视为已存在（否则会重复插入同一标识）")
	}

	// ---- 4. 参数校验 ----
	if err := st.SaveItems(ctx, items[:1], vectors[:2]); err == nil {
		t.Error("items/vectors 长度不一致时应报错")
	}
	if err := st.SaveItems(ctx, nil, nil); err != nil {
		t.Errorf("空输入应直接返回 nil: %v", err)
	}

	// ---- 5. 复用已存在的集合再打开一次：应走"集合已存在"分支并仍可用 ----
	st2, err := Open(ctx, Options{DSN: dsn, MilvusAddress: milvusAddr, Collection: collection, VecDim: dim})
	if err != nil {
		t.Fatalf("复用已存在集合时 Open 失败: %v", err)
	}
	defer st2.Close()

	existing, err = st2.ExistingIdentifiers(ctx, []string{items[1].Url})
	if err != nil {
		t.Fatalf("复用集合后 ExistingIdentifiers: %v", err)
	}
	if _, ok := existing[items[1].Url]; !ok {
		t.Error("复用已存在集合后查询不到已写入的标识")
	}

	// ---- 6. 连接失败路径不应 panic（DSN 指向不存在的库）----
	if _, err := Open(ctx, Options{
		DSN:           "nobody:nobody@tcp(127.0.0.1:3306)/litstream_searcher_no_such_db?charset=utf8mb4",
		MilvusAddress: milvusAddr,
		Collection:    collection,
		VecDim:        dim,
	}); err == nil {
		t.Error("无效 DSN 时 Open 应报错")
	}

	// ---- 6b. Milvus 不可达时快速失败，错误里要能看出地址与原因 ----
	// 端口 1 必然没有服务；ConnectTimeout 压到 1s 让用例不至于等太久。
	if _, err := Open(ctx, Options{
		DSN:            dsn,
		MilvusAddress:  "127.0.0.1:1",
		Collection:     collection,
		VecDim:         dim,
		ConnectTimeout: time.Second,
	}); err == nil {
		t.Error("Milvus 不可达时 Open 应报错")
	} else if !strings.Contains(err.Error(), "Milvus") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("错误信息应同时包含 Milvus 与地址，便于定位: %v", err)
	}

	// ---- 7. 记录数 / 日期分布 / 向量数 ----
	// 此时 items[0] 已被软删除：计数口径包含软删除行（与去重口径一致）。
	counted, err := st2.CountItems(ctx)
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if counted.Total != 3 || counted.Deleted != 1 {
		t.Errorf("CountItems = %+v, want Total=3 Deleted=1", counted)
	}

	dates, err := st2.ListItemDates(ctx)
	if err != nil {
		t.Fatalf("ListItemDates: %v", err)
	}
	if len(dates) != 1 {
		t.Fatalf("ListItemDates 返回 %d 个日期, want 1: %+v", len(dates), dates)
	}
	if dates[0].PublishDate != "2024-06-01" || dates[0].Total != 3 || dates[0].Deleted != 1 {
		t.Errorf("ListeItemDates[0] = %+v, want {2024-06-01 3 1}", dates[0])
	}

	vectorsBefore, err := st2.VectorCount(ctx)
	if err != nil {
		t.Fatalf("VectorCount: %v", err)
	}
	if vectorsBefore != 3 {
		t.Errorf("VectorCount = %d, want 3", vectorsBefore)
	}

	// ---- 8. 清空：MySQL 硬删除（含软删除行）+ Milvus 集合重建 ----
	purged, err := st2.PurgeAll(ctx)
	if err != nil {
		t.Fatalf("PurgeAll: %v", err)
	}
	if purged.MySQLDeleted != 3 {
		t.Errorf("清空 MySQL %d 条, want 3（软删除行也应是硬删除）", purged.MySQLDeleted)
	}
	if purged.MilvusVectors != 3 || purged.Collection != collection {
		t.Errorf("PurgeResult = %+v", purged)
	}

	if counted, err = st2.CountItems(ctx); err != nil {
		t.Fatalf("清空后 CountItems: %v", err)
	} else if counted.Total != 0 || counted.Deleted != 0 {
		t.Errorf("清空后仍有记录: %+v", counted)
	}
	if dates, err = st2.ListItemDates(ctx); err != nil {
		t.Fatalf("清空后 ListItemDates: %v", err)
	} else if len(dates) != 0 {
		t.Errorf("清空后仍有日期记录: %+v", dates)
	}
	if vectors, err := st2.VectorCount(ctx); err != nil {
		t.Fatalf("清空后 VectorCount: %v", err)
	} else if vectors != 0 {
		t.Errorf("清空后 Milvus 仍有 %d 条向量", vectors)
	}

	// ---- 9. 清空后仍能正常写入（集合已重建且已加载）----
	after := []domain.Item{
		newItem("http://arxiv.org/abs/2406.00011v1", "Paper After Purge"),
	}
	if err := st2.SaveItems(ctx, after, [][]float32{{0, 0, 1, 1}}); err != nil {
		t.Fatalf("清空后 SaveItems: %v", err)
	}
	if after[0].ID == 0 {
		t.Error("清空后写入未回填自增 ID")
	}
	existing, err = st2.ExistingIdentifiers(ctx, []string{after[0].Url})
	if err != nil {
		t.Fatalf("清空后 ExistingIdentifiers: %v", err)
	}
	if _, ok := existing[after[0].Url]; !ok {
		t.Error("清空后写入的标识查询不到")
	}
	if vectors, err := st2.VectorCount(ctx); err != nil {
		t.Fatalf("清空后 VectorCount: %v", err)
	} else if vectors != 1 {
		t.Errorf("清空后写入 1 条，VectorCount = %d, want 1", vectors)
	}
}

func newItem(url, title string) domain.Item {
	return domain.Item{
		Title:       title,
		WebSource:   "arxiv",
		Url:         url,
		PublishDate: "2024-06-01",
		Authors:     []string{"Alice"},
		Abstract:    "abstract of " + title,
		Fields:      []string{"cs.CL"},
		Keywords:    []string{},
		Summary:     "summary of " + title,
	}
}

// dropTestCollection 用一条独立连接删除测试集合（t.Cleanup 阶段 st 已关闭）。
func dropTestCollection(t *testing.T, addr, collection string) {
	t.Helper()
	cli, err := client.NewClient(context.Background(), client.Config{Address: addr})
	if err != nil {
		t.Logf("清理测试集合失败（请手动删除 %s）: %v", collection, err)
		return
	}
	defer cli.Close()
	if err := cli.DropCollection(context.Background(), collection); err != nil {
		t.Logf("清理测试集合失败（请手动删除 %s）: %v", collection, err)
	}
}

// makeVectors 生成 n 条 dim 维的单位向量（维度由 SEARCHER_IT_VEC_DIM 决定）。
func makeVectors(dim, n int) [][]float32 {
	out := make([][]float32, n)
	for i := range out {
		vec := make([]float32, dim)
		if dim > 0 {
			vec[i%dim] = 1
		}
		out[i] = vec
	}
	return out
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s = %q 不是整数", key, raw)
	}
	return v
}

// TestIntegrationDeleteByDate 按日期删除的完整验证（真实 MySQL + Milvus）。
//
// 与上一个用例不同，这个用例可以指向非一次性的库，因此对数据自我约束：
// 用独立的集合名（SEARCHER_IT_COLLECTION），并且只操作带固定 url 前缀的记录。
// 运行方式（数据库可以共用，集合必须是独立的一次性集合）：
//
//	SEARCHER_IT_DSN='user:pass@tcp(127.0.0.1:3306)/litstream?charset=utf8mb4&parseTime=True&loc=Local' \
//	SEARCHER_IT_MILVUS_ADDRESS=127.0.0.1:19530 \
//	SEARCHER_IT_COLLECTION=litstream_searcher_it_delete \
//	SEARCHER_IT_VEC_DIM=4 \
//	go test ./store/ -run TestIntegrationDeleteByDate -v
func TestIntegrationDeleteByDate(t *testing.T) {
	dsn := os.Getenv("SEARCHER_IT_DSN")
	milvusAddr := os.Getenv("SEARCHER_IT_MILVUS_ADDRESS")
	collection := os.Getenv("SEARCHER_IT_COLLECTION")
	dim := envInt(t, "SEARCHER_IT_VEC_DIM", 4)
	if dsn == "" || milvusAddr == "" || collection == "" {
		t.Skip("未设置 SEARCHER_IT_* 环境变量，跳过集成用例")
	}

	const (
		keepDate = "2031-01-01" // 用未来日期，避免与真实数据互相干扰
		dropDate = "2031-01-02"
		prefix   = "http://arxiv.org/abs/IT-DELETE-"
	)

	ctx := context.Background()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接 MySQL: %v", err)
	}

	// 前置检查：清理上次运行可能留下的、属于本用例的记录（只按前缀，不碰其他数据）。
	cleanup := func() {
		if err := db.Unscoped().
			Where("url LIKE ?", prefix+"%").
			Delete(&domain.Item{}).Error; err != nil {
			t.Logf("清理测试记录失败: %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	st, err := Open(ctx, Options{DSN: dsn, MilvusAddress: milvusAddr, Collection: collection, VecDim: dim})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	// 用独立集合，结束后删掉，避免影响真实检索。
	// 这里单独建连接：t.Cleanup 的执行顺序在 st.Close() 之后（LIFO），
	// 复用 st 的客户端只会拿到 "client connection is closing"。
	t.Cleanup(func() {
		dropTestCollection(t, milvusAddr, collection)
	})

	// ---- 准备：两天各两条 ----
	items := []domain.Item{
		newItem(prefix+"1", "Drop A"),
		newItem(prefix+"2", "Drop B"),
		newItem(prefix+"3", "Keep C"),
	}
	items[0].PublishDate, items[1].PublishDate = dropDate, dropDate
	items[2].PublishDate = keepDate
	vectors := makeVectors(dim, 3)

	if err := st.SaveItems(ctx, items, vectors); err != nil {
		t.Fatalf("SaveItems: %v", err)
	}

	if n, err := st.CountItemsByDate(ctx, dropDate); err != nil || n != 2 {
		t.Fatalf("CountItemsByDate(%s) = %d, %v; want 2", dropDate, n, err)
	}
	if n, err := st.CountItemsByDate(ctx, keepDate); err != nil || n != 1 {
		t.Fatalf("CountItemsByDate(%s) = %d, %v; want 1", keepDate, n, err)
	}

	// ---- 删除前先软删一条 dropDate 的记录：统计口径必须包含软删除行 ----
	if err := db.Where("url = ?", items[0].Url).Delete(&domain.Item{}).Error; err != nil {
		t.Fatalf("软删除: %v", err)
	}
	if n, err := st.CountItemsByDate(ctx, dropDate); err != nil || n != 2 {
		t.Fatalf("软删除后 CountItemsByDate(%s) = %d, %v; want 2（应包含软删除行）", dropDate, n, err)
	}

	// ---- 删除该日期 ----
	result, err := st.DeleteByDate(ctx, dropDate)
	if err != nil {
		t.Fatalf("DeleteByDate: %v", err)
	}
	if result.MySQLDeleted != 2 {
		t.Errorf("MySQLDeleted = %d, want 2（含已软删除行，硬删除）", result.MySQLDeleted)
	}
	if result.MilvusDeleted != 2 {
		t.Errorf("MilvusDeleted = %d, want 2", result.MilvusDeleted)
	}
	if result.PublishDate != dropDate {
		t.Errorf("PublishDate = %q, want %q", result.PublishDate, dropDate)
	}

	// MySQL：被删日期的行彻底消失（Unscoped 也查不到），另一天不受影响
	if n, err := st.CountItemsByDate(ctx, dropDate); err != nil || n != 0 {
		t.Errorf("删除后 CountItemsByDate(%s) = %d, %v; want 0", dropDate, n, err)
	}
	if n, err := st.CountItemsByDate(ctx, keepDate); err != nil || n != 1 {
		t.Errorf("删除后 CountItemsByDate(%s) = %d, %v; want 1（其他日期不应受影响）", keepDate, n, err)
	}

	// Milvus：被删的主键查不到，保留的那条还在
	if err := st.milvus.Flush(ctx, collection, false); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	keptIDs := []int64{int64(items[2].ID)}
	res, err := st.milvus.QueryByPks(ctx, collection, nil, entity.NewColumnInt64("id", keptIDs), []string{"id"})
	if err != nil {
		t.Fatalf("QueryByPks(保留): %v", err)
	}
	if res.Len() != 1 {
		t.Errorf("保留日期的向量数 = %d, want 1", res.Len())
	}

	droppedIDs := []int64{int64(items[0].ID), int64(items[1].ID)}
	res, err = st.milvus.QueryByPks(ctx, collection, nil, entity.NewColumnInt64("id", droppedIDs), []string{"id"})
	if err != nil {
		t.Fatalf("QueryByPks(已删): %v", err)
	}
	if res.Len() != 0 {
		t.Errorf("被删日期的向量仍有 %d 条, want 0（否则检索会命中已删除的数据）", res.Len())
	}

	// ---- 删除后可重新收集：同标识能再次写入且不产生重复向量 ----
	readded := []domain.Item{
		newItem(prefix+"1", "Drop A again"),
		newItem(prefix+"2", "Drop B again"),
	}
	readded[0].PublishDate, readded[1].PublishDate = dropDate, dropDate

	existing, err := st.ExistingIdentifiers(ctx, []string{readded[0].Url, readded[1].Url})
	if err != nil {
		t.Fatalf("ExistingIdentifiers: %v", err)
	}
	if len(existing) != 0 {
		t.Fatalf("硬删除后标识仍被视为已存在: %v（软删除会导致再也抓不回来）", existing)
	}
	if err := st.SaveItems(ctx, readded, makeVectors(dim, 2)); err != nil {
		t.Fatalf("重新写入: %v", err)
	}
	if n, err := st.CountItemsByDate(ctx, dropDate); err != nil || n != 2 {
		t.Errorf("重新收集后 CountItemsByDate(%s) = %d, %v; want 2", dropDate, n, err)
	}
}

// ---- 按日期重新生成（regen-*）的存储用例 ----
//
// 这组用例会真实连接 MySQL 与 Milvus（复用上面的 SEARCHER_IT_* 约定），
// 用一个独立的库与集合，不碰生产数据。

// regenStore 打开一个用于"重新生成"用例的 Store，并清理起点。
// 返回的 cleanup 会删掉本次插入的行与测试集合。
func regenStore(t *testing.T) (*Store, context.Context, func()) {
	t.Helper()

	dsn := os.Getenv("SEARCHER_IT_DSN")
	milvusAddr := os.Getenv("SEARCHER_IT_MILVUS_ADDRESS")
	collection := os.Getenv("SEARCHER_IT_COLLECTION")
	if dsn == "" || milvusAddr == "" || collection == "" {
		t.Skip("未设置 SEARCHER_IT_* 环境变量，跳过集成用例")
	}
	ctx := context.Background()

	st, err := Open(ctx, Options{DSN: dsn, MilvusAddress: milvusAddr, Collection: collection, VecDim: 4})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cleanup := func() {
		// 先删测试行，再删集合，避免留下半截数据。
		if err := st.db.WithContext(ctx).Unscoped().Where("publish_date = ?", regenTestDate).Delete(&domain.Item{}).Error; err != nil {
			t.Logf("清理测试行失败: %v", err)
		}
		if err := st.dropCollection(ctx); err != nil {
			t.Logf("清理测试集合失败: %v", err)
		}
		if err := st.Close(); err != nil {
			t.Logf("关闭连接失败: %v", err)
		}
	}
	// 起点干净：去掉可能残留的同日期数据。
	if err := st.db.WithContext(ctx).AutoMigrate(&domain.Item{}); err != nil {
		cleanup()
		t.Fatalf("建表: %v", err)
	}
	if err := st.db.WithContext(ctx).Unscoped().Where("publish_date = ?", regenTestDate).Delete(&domain.Item{}).Error; err != nil {
		cleanup()
		t.Fatalf("清理起点: %v", err)
	}
	return st, ctx, cleanup
}

const regenTestDate = "2024-06-20"

// seedRegenItems 插入 n 条指定日期的记录，返回带自增 ID 的记录。
func seedRegenItems(t *testing.T, st *Store, ctx context.Context, n int) []domain.Item {
	t.Helper()

	items := make([]domain.Item, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, domain.Item{
			Title:           "Title " + strconv.Itoa(i),
			Url:             "http://arxiv.org/abs/regen." + strconv.Itoa(i),
			PublishDate:     regenTestDate,
			Abstract:        "Abstract " + strconv.Itoa(i),
			Summary:         "old summary " + strconv.Itoa(i),
			TitleChinese:    "旧标题 " + strconv.Itoa(i),
			AbstractChinese: "旧摘要 " + strconv.Itoa(i),
		})
	}
	if err := st.db.WithContext(ctx).Create(&items).Error; err != nil {
		t.Fatalf("插入测试记录: %v", err)
	}
	return items
}

// TestIntegrationListItemsByDate 按日期取出记录，字段要完整（重算依赖原始 title/abstract）。
func TestIntegrationListItemsByDate(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 3)

	got, err := st.ListItemsByDate(ctx, regenTestDate)
	if err != nil {
		t.Fatalf("ListItemsByDate: %v", err)
	}
	if len(got) != len(seeded) {
		t.Fatalf("取出 %d 条, want %d", len(got), len(seeded))
	}
	for i, item := range got {
		want := seeded[i]
		if item.ID != want.ID {
			t.Errorf("第 %d 条 id = %d, want %d", i, item.ID, want.ID)
		}
		if item.Title != want.Title || item.Abstract != want.Abstract || item.Summary != want.Summary {
			t.Errorf("第 %d 条字段不完整: %+v", i, item)
		}
		if item.PublishDate != regenTestDate {
			t.Errorf("第 %d 条 publish_date = %q", i, item.PublishDate)
		}
	}

	// 不存在的日期返回空
	none, err := st.ListItemsByDate(ctx, "1999-01-01")
	if err != nil {
		t.Fatalf("ListItemsByDate(空日期): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("不存在的日期应返回空, 实际 %d 条", len(none))
	}
}

// TestIntegrationUpdateTranslations 中文翻译要按主键精确回写，且不动别的字段。
func TestIntegrationUpdateTranslations(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 2)

	updates := []storefields.TranslationUpdate{
		{ID: seeded[0].ID, TitleChinese: "新标题一", AbstractChinese: "新摘要一", IsChinese: true},
		{ID: seeded[1].ID, TitleChinese: "新标题二", AbstractChinese: "新摘要二", IsChinese: false},
	}
	written, err := st.UpdateTranslations(ctx, updates)
	if err != nil {
		t.Fatalf("UpdateTranslations: %v", err)
	}
	if written != 2 {
		t.Errorf("更新 %d 行, want 2", written)
	}

	got, err := st.ListItemsByDate(ctx, regenTestDate)
	if err != nil {
		t.Fatalf("ListItemsByDate: %v", err)
	}
	for _, item := range got {
		switch item.ID {
		case seeded[0].ID:
			if item.TitleChinese != "新标题一" || item.AbstractChinese != "新摘要一" {
				t.Errorf("id=%d 译文未更新: %q/%q", item.ID, item.TitleChinese, item.AbstractChinese)
			}
			if !item.IsChinese {
				t.Error("is_chinese 应为 true")
			}
		case seeded[1].ID:
			if item.TitleChinese != "新标题二" || item.IsChinese {
				t.Errorf("id=%d 译文/is_chinese 不对: %q/%v", item.ID, item.TitleChinese, item.IsChinese)
			}
		}
		// 关键：不该顺手改动原始字段
		if item.Title == "" || item.Abstract == "" || item.Summary == "" {
			t.Errorf("id=%d 的原始字段被清掉了: title=%q abstract=%q summary=%q",
				item.ID, item.Title, item.Abstract, item.Summary)
		}
	}
}

// TestIntegrationUpdateSummaries summary 要按主键回写，且不动标题与中文字段。
func TestIntegrationUpdateSummaries(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 2)

	written, err := st.UpdateSummaries(ctx, []storefields.SummaryUpdate{
		{ID: seeded[0].ID, Summary: "全新 summary 一"},
		{ID: seeded[1].ID, Summary: "全新 summary 二"},
	})
	if err != nil {
		t.Fatalf("UpdateSummaries: %v", err)
	}
	if written != 2 {
		t.Errorf("更新 %d 行, want 2", written)
	}

	got, err := st.ListItemsByDate(ctx, regenTestDate)
	if err != nil {
		t.Fatalf("ListItemsByDate: %v", err)
	}
	for _, item := range got {
		want := "全新 summary " + map[uint]string{seeded[0].ID: "一", seeded[1].ID: "二"}[item.ID]
		if item.Summary != want {
			t.Errorf("id=%d summary = %q, want %q", item.ID, item.Summary, want)
		}
		if item.TitleChinese == "" {
			t.Errorf("id=%d 的中文字段被清掉了", item.ID)
		}
	}
}

// TestIntegrationUpdateRowsKeepUntouchedColumns 更新只碰指定列：并发期间别人改过的
// 其他字段不能被覆盖回旧值（这正是用 Updates(map) 而不是 Save(整行) 的原因）。
func TestIntegrationUpdateRowsKeepUntouchedColumns(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 1)
	id := seeded[0].ID

	// 模拟"重新生成期间另一处改了 keywords"。
	// 用 struct + Select 才能走字段的 serializer：直接传 map 会绕过 JSON 序列化，
	// 而 keywords 是 type:json 列，塞裸字符串会被 MySQL 以 3140 拒绝。
	if err := st.db.WithContext(ctx).Model(&domain.Item{}).Where("id = ?", id).
		Select("keywords").Updates(&domain.Item{Keywords: []string{"changed"}}).Error; err != nil {
		t.Fatalf("改 keywords: %v", err)
	}

	if _, err := st.UpdateSummaries(ctx, []storefields.SummaryUpdate{{ID: id, Summary: "新 summary"}}); err != nil {
		t.Fatalf("UpdateSummaries: %v", err)
	}

	var got domain.Item
	if err := st.db.WithContext(ctx).First(&got, id).Error; err != nil {
		t.Fatalf("读回记录: %v", err)
	}
	if len(got.Keywords) != 1 || got.Keywords[0] != "changed" {
		t.Errorf("keywords = %v, want [changed]（不该被 summary 更新覆盖）", got.Keywords)
	}
	if got.Summary != "新 summary" {
		t.Errorf("summary = %q", got.Summary)
	}
}

// TestIntegrationReplaceVectors 向量替换：新向量要能被检索到，且同 id 不会出现两条。
func TestIntegrationReplaceVectors(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 3)

	// 先写入一批"旧"向量
	old := make([]storefields.VectorUpdate, 0, len(seeded))
	for i, item := range seeded {
		old = append(old, storefields.VectorUpdate{ID: item.ID, Vec: []float32{float32(i + 1), 0, 0, 0}})
	}
	if _, err := st.ReplaceVectors(ctx, old); err != nil {
		t.Fatalf("首次写入向量: %v", err)
	}

	// 再用可区分的新向量替换
	inFlight, err := st.VectorCount(ctx)
	if err != nil {
		t.Fatalf("VectorCount: %v", err)
	}
	if inFlight != int64(len(seeded)) {
		t.Fatalf("写入 %d 条向量, want %d", inFlight, len(seeded))
	}

	fresh := make([]storefields.VectorUpdate, 0, len(seeded))
	for _, item := range seeded {
		fresh = append(fresh, storefields.VectorUpdate{ID: item.ID, Vec: []float32{0, float32(item.ID), 0, 0}})
	}
	written, err := st.ReplaceVectors(ctx, fresh)
	if err != nil {
		t.Fatalf("ReplaceVectors: %v", err)
	}
	if written != int64(len(fresh)) {
		t.Errorf("替换 %d 条, want %d", written, len(fresh))
	}

	// 替换后总数不变（说明旧向量被删掉了，没有新旧并存）
	after, err := st.VectorCount(ctx)
	if err != nil {
		t.Fatalf("VectorCount: %v", err)
	}
	if after != int64(len(seeded)) {
		t.Errorf("替换后向量数 = %d, want %d（同 id 不该有两条向量）", after, len(seeded))
	}
}

// TestIntegrationReplaceVectorsDimensionMismatch 维度不符要整批拒绝，不写任何向量。
func TestIntegrationReplaceVectorsDimensionMismatch(t *testing.T) {
	st, ctx, cleanup := regenStore(t)
	defer cleanup()

	seeded := seedRegenItems(t, st, ctx, 1)

	_, err := st.ReplaceVectors(ctx, []storefields.VectorUpdate{
		{ID: seeded[0].ID, Vec: []float32{1, 2, 3}}, // 集合维度是 4
	})
	if err == nil {
		t.Fatal("维度不符时期望报错")
	}
	if !strings.Contains(err.Error(), "维度") {
		t.Errorf("错误信息应说明维度问题，实际: %v", err)
	}

	count, err := st.VectorCount(ctx)
	if err != nil {
		t.Fatalf("VectorCount: %v", err)
	}
	if count != 0 {
		t.Errorf("维度不符时不该写入向量, 实际 %d 条", count)
	}
}
