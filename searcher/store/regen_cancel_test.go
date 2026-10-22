package store

import (
	"context"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"searcher/domain"
)

// TestCancelledContextSurfacesError 确认 ctx 已取消时查询会**报错**，而不是
// 悄悄返回空结果。
//
// 这一点很重要：regen.Run 把"查出 0 条"当作"该日期没有记录"并正常退出。
// 如果取消被吞掉、只剩空结果，一次 Ctrl-C 或上游超时就会被显示成"没有记录，
// 无需重新生成"——看起来跑成功了，实际一条都没处理。
//
// 实测 gorm 会正常返回 context canceled；regen.Run 里另外还有一道显式 ctx.Err()
// 检查作为纵深防御，防止依赖库行为变化后静默成功。
func TestCancelledContextSurfacesError(t *testing.T) {
	dsn := os.Getenv("SEARCHER_IT_DSN")
	if dsn == "" {
		t.Skip("未设置 SEARCHER_IT_DSN，跳过")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("连 MySQL: %v", err)
	}
	if err := db.AutoMigrate(&domain.Item{}); err != nil {
		t.Fatalf("建表: %v", err)
	}
	if err := db.Create(&domain.Item{Title: "cancel-probe", PublishDate: "2024-06-20"}).Error; err != nil {
		t.Fatalf("预置: %v", err)
	}
	defer db.Unscoped().Where("title = ?", "cancel-probe").Delete(&domain.Item{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 查询前就已取消

	var items []domain.Item
	err = db.WithContext(ctx).Unscoped().Where("publish_date = ?", "2024-06-20").Find(&items).Error
	t.Logf("已取消的 ctx 查询结果: rows=%d err=%v", len(items), err)

	if err == nil {
		t.Errorf("ctx 已取消时查询应报错（否则会被误判成'该日期没有记录'而静默成功），实际 rows=%d err=nil", len(items))
	} else {
		t.Logf("符合预期：ctx 已取消时查询报错 %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("ctx 应为已取消状态")
	}
}
