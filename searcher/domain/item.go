// Package domain 定义入库实体，与 litstream 后端共享同一张 MySQL 表。
package domain

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Item 与 litstream/domain.Item 保持完全相同的结构（列名、json 类型、gorm tag 一致），
// 从而写入同一张 MySQL 表 `items`。改了 litstream 侧结构时记得同步这里。
type Item struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Title           string   `gorm:"column:title;" json:"title"`
	WebSource       string   `gorm:"column:web_source;" json:"web_source"`
	Url             string   `gorm:"column:url;" json:"url"`
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

// Identifier 返回 Item 的唯一标识，用于入库前按标识去重。
//
// items 表没有单独的 arxiv_id 列：抓取时把 arXiv 的 ArxivID（形如
// "http://arxiv.org/abs/2406.00001v1"）写入 Url 列，因此 Url 即唯一标识。
// 这里只裁剪首尾空白，不做其他改写，保证与库中已写入的值逐字比较。
// 返回空串表示该条没有可用标识，调用方应视为"无法判定重复"而保留。
func (it Item) Identifier() string {
	return strings.TrimSpace(it.Url)
}
