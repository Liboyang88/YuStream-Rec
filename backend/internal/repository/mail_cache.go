package repository

import (
	"context"
	"time"

	"litstream/domain"

	"github.com/redis/go-redis/v9"
)

// RecommendCache 推荐结果缓存：按"用户 + 随机 key"存取某一轮已生成的精排结果。
// 前端持有的是随机 key（suffix），完整 redis key 由 domain.RecommendAddress(uid, key) 拼出，
// 保证不同用户的 key 隔离、同一个 key 只属于一个用户。
type MailCache struct {
	redisdb *redis.Client
	ttl     time.Duration // 结果保留时长，过期即视为"未命中"
}

// NewMailCache 初始化邮箱验证码存
func NewMailCache(redisdb *redis.Client, ttl time.Duration) *MailCache {
	return &MailCache{
		redisdb: redisdb,
		ttl:     ttl,
	}
}

func (m *MailCache) Set(ctx context.Context, email string, value string) error {
	return m.redisdb.Set(ctx, domain.KeyEmailSend(email), value, m.ttl).Err()
}

func (m *MailCache) Ttl(ctx context.Context, email string) (int, error) {
	duration, err := m.redisdb.TTL(ctx, domain.KeyEmailSend(email)).Result()
	// duration==-2过期；==-1不存在 统一返回-1
	if duration < 0 {
		return -1, nil
	}
	seconds := int(duration / time.Second)
	return seconds, err
}

func (m *MailCache) Get(ctx context.Context, email string) (string, error) {
	code, err := m.redisdb.Get(ctx, domain.KeyEmailSend(email)).Result()
	if err != nil {
		return "", nil
	}
	return code, err
}

func (m *MailCache) Verify(ctx context.Context, email string, thisCode string) (bool, error) {
	verifyCode, err := m.Get(ctx, email)
	if err != nil {
		return false, err
	}
	return verifyCode == thisCode, nil
}
