package main

import (
	"time"

	"gorm.io/gorm"
)

// Item 与 dcpro/domain.Item 保持完全相同的结构（列名、json 类型、gorm tag 一致），
// 从而写入同一张 MySQL 表 `items`。改了 dcpro 侧结构时记得同步这里。
type Item struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Title           string   `gorm:"column:title;" json:"title"`
	WebSource       string   `gorm:"column:web_source;" json:"web_source"`
	PublishDate     string   `gorm:"column:publish_date;" json:"publish_date"`
	Authors         []string `gorm:"column:authors;type:json;serializer:json" json:"authors"`
	Abstract        string   `gorm:"column:abstract;type:text" json:"abstract"`
	Fields          []string `gorm:"column:fields;type:json;serializer:json" json:"fields"`
	Keywords        []string `gorm:"column:keywords;type:json;serializer:json" json:"keywords"`
	IsChinese       bool     `gorm:"column:is_chinese" json:"is_chinese"`
	TitleChinese    string   `gorm:"column:title_chinese;" json:"title_chinese"`
	AbstractChinese string   `gorm:"column:abstract_chinese;type:text" json:"abstract_chinese"`
	Summary         string   `gorm:"column:summary;type:text" json:"summary"`
}
