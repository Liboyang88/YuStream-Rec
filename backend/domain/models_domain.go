package domain

import (
	"time"
	"strconv"
	"encoding/json"

	"gorm.io/gorm"
)
type User struct {
	ID        uint `gorm:"primarykey" json:"id" mapstructure:"id"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Username string `gorm:"column:username;size:15;not null" json:"username"`
	Email    string `gorm:"column:email;size:50;uniqueIndex;not null" json:"email"`
	Password string `gorm:"column:password;size:60;not null" json:"-"`
	Age      int    `gorm:"column:age" json:"age"`
	Gender   string `gorm:"column:gender;size:10" json:"gender"`
	Job      string `gorm:"column:job;size:20" json:"job"`
	Education string `gorm:"column:education;size:20" json:"education"`
	Location string `gorm:"column:location;size:20" json:"location"`
	Organization string `gorm:"column:organization;size:20" json:"organization"`
	InterestingFields []string `gorm:"column:interesting_fields;type:json;serializer:json" json:"interesting_fields"`
	Summary string `gorm:"column:summary;type:text" json:"-"`
	Topics string `gorm:"column:topics;type:text" json:"-"`
}

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

type InteractionType string
const(
    InteractionTypeView InteractionType = "view"
    InteractionTypeClick InteractionType = "click"
    InteractionTypeScroll InteractionType = "scroll"
    InteractionTypeLike InteractionType = "like"
)

type Interaction struct{
    Type InteractionType `json:"type"`
    Target Item `json:"target" binding:"required"`
    Detail string `json:"detail" binding:"omitempty,min=1,max=300"`
    LongTS int64 `json:"long_ts" binding:"omitempty"`
}

func UserToHash(user *User) (map[string]string, error) {
    hash := make(map[string]string)

    // 基础字段转字符串
    hash["id"] = strconv.FormatUint(uint64(user.ID), 10)
    hash["username"] = user.Username
    hash["age"] = strconv.Itoa(user.Age)
    hash["gender"] = user.Gender
    hash["job"] = user.Job
    hash["education"] = user.Education
    hash["location"] = user.Location
    hash["organization"] = user.Organization
    hash["summary"] = user.Summary
	hash["topics"] = user.Topics

	if user.InterestingFields == nil {
		user.InterestingFields = []string{}
	}

    // []string 数组序列化 JSON 存入 hash
    fieldBytes, err := json.Marshal(user.InterestingFields)
    if err != nil {
        return nil, err
    }
    hash["interesting_fields"] = string(fieldBytes)

    // Password 不放入Redis，安全隔离
    return hash, nil
}

func HashToUser(hash map[string]string, user *User) error {
	// 空 hash = 缓存未命中，直接返回，由调用方决定是否回退 MySQL
	if len(hash) == 0 {
		return nil
	}

	// id 字符串转 uint
	id, err := strconv.ParseUint(hash["id"], 10, 64)
	if err != nil {
		return err
	}
	user.ID = uint(id)

	user.Username = hash["username"]
	age, err := strconv.Atoi(hash["age"])
	if err != nil {
		return err
	}
	user.Age = age
	user.Gender = hash["gender"]
	user.Job = hash["job"]
	user.Education = hash["education"]
	user.Location = hash["location"]
	user.Organization = hash["organization"]
	user.Summary = hash["summary"]
	user.Topics = hash["topics"]

	// JSON字符串反序列化为 []string
	if hash["interesting_fields"] == "" {
		user.InterestingFields = []string{}
		return nil
	}
	var fields []string
	if err := json.Unmarshal([]byte(hash["interesting_fields"]), &fields); err != nil {
		return err
	}
	user.InterestingFields = fields

	return nil
}