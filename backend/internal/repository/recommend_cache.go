package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"litstream/domain"

	"github.com/redis/go-redis/v9"
)

// RecommendCache 推荐结果缓存：按"用户 + 随机 key"存取某一轮已生成的精排结果。
// 前端持有的是随机 key（suffix），完整 redis key 由 domain.RecommendAddress(uid, key) 拼出，
// 保证不同用户的 key 隔离、同一个 key 只属于一个用户。
type RecommendCache struct {
	redisdb *redis.Client
	ttl     time.Duration // 结果保留时长，过期即视为"未命中"
}

type payload struct {
	Items   []domain.Item
	Outline string
}

// NewRecommendCache 构造推荐结果缓存。
func NewRecommendCache(redisdb *redis.Client, ttl time.Duration) *RecommendCache {
	return &RecommendCache{
		redisdb: redisdb,
		ttl:     ttl,
	}
}

// Get 读取指定 key 下已生成好的精排结果。
// 返回约定：key 为空 / key 不存在（redis.Nil）/ 内容为空 → (nil, nil)，表示"未命中"；
// 命中返回 (items, nil)；redis 或反序列化出错才返回错误。
func (c *RecommendCache) Get(ctx context.Context, uid uint) ([]domain.Item, string, error) {
	raw, err := c.redisdb.Get(ctx, domain.RecommendAddress(uid)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}

	var pl payload
	if err := json.Unmarshal([]byte(raw), &pl); err != nil {
		return nil, "", err
	}
	if len(pl.Items) == 0 {
		return nil, "", err
	}
	return pl.Items, pl.Outline, nil
}

// Set 把一轮精排结果写入指定 key 并设 TTL。空结果不落盘（保持"无 key = 未命中"语义）。
func (c *RecommendCache) Set(ctx context.Context, uid uint, items []domain.Item, outline string) error {
	if len(items) == 0 {
		return nil
	}

	buf, err := json.Marshal(payload{Items: items, Outline: outline})
	if err != nil {
		return err
	}
	return c.redisdb.Set(ctx, domain.RecommendAddress(uid), buf, c.ttl).Err()
}

var countScript = redis.NewScript(`
	local newVal = redis.call('INCR', KEYS[1])
	if newVal >= 5 then
		redis.call('SET', KEYS[1], 0)
	end
	return newVal
	`)

func (c *RecommendCache) Count(ctx context.Context, uid uint) (bool, error) {
	// 执行脚本，KEYS[1] = recommend:count
	res, err := countScript.Run(ctx, c.redisdb, []string{domain.RecommendCount(uid)}).Int64()
	if err != nil {
		return false, err
	}
	return res >= 5, nil
}
