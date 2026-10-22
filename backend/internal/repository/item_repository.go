package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"litstream/db"
	"litstream/domain"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ItemRepository 物品数据访问：Milvus 向量检索 + MySQL 明细 + Redis 明细缓存 + 最近推荐 zset。
type ItemRepository struct {
	recsWindow int // 推荐去重窗口：score 0..recsWindow-1 的成员视为"最近推荐过"
	itemTTL    time.Duration
	sqldb      *gorm.DB
	milvus     *db.MilvusDB
	redisdb    *redis.Client
}

// NewItemRepository 构造物品仓储。
// recsWindow 对应 env.RecsWindow。
func NewItemRepository(
	recsWindow int, sqldb *gorm.DB,
	milvus *db.MilvusDB, redisdb *redis.Client,
	itemTTL time.Duration) *ItemRepository {
	return &ItemRepository{
		recsWindow: recsWindow,
		itemTTL:    itemTTL,
		sqldb:      sqldb,
		milvus:     milvus,
		redisdb:    redisdb,
	}
}

// RecentExcludes 读取最近推荐过的物品 ID（score 0..recsWindow-1），推荐时用于排除。
func (r *ItemRepository) RecentExcludes(ctx context.Context, userID uint) ([]uint, error) {
	key := domain.RecentlyRecommended(userID)
	members, err := r.redisdb.ZRangeByScore(ctx, key,
		&redis.ZRangeBy{Min: "0", Max: strconv.Itoa(r.recsWindow - 1)}).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(members))
	for _, m := range members {
		id, err := strconv.ParseUint(m, 10, 64)
		if err != nil {
			continue // 脏数据跳过
		}
		ids = append(ids, uint(id))
	}
	return ids, nil
}

// SearchVec 在 Milvus 中按向量检索 topK 个候选 ID，可用 excludeIDs 过滤。
func (r *ItemRepository) SearchVec(ctx context.Context, vec []float32, topK int, excludeIDs []uint) ([]uint, error) {
	return r.milvus.SearchVec(ctx, &vec, topK, excludeIDs)
}

// LoadMany 按给定 ID 顺序批量加载物品明细：Redis MGet 命中直取，未命中回退 MySQL 并回填缓存。
// 返回顺序与入参 ids 一致；MySQL 中不存在的 ID 会被跳过。
func (r *ItemRepository) LoadMany(ctx context.Context, ids []uint) ([]domain.Item, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, domain.KeyItemIDToItem(id))
	}
	cacheValues, err := r.redisdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	byID := make(map[uint]domain.Item, len(ids))
	var missIDs []uint
	for i, v := range cacheValues {
		rawStr, ok := v.(string) // MGet 缺失 key 返回 nil，必须先类型断言
		if !ok || rawStr == "" {
			missIDs = append(missIDs, ids[i])
			continue
		}
		var item domain.Item
		if err := json.Unmarshal([]byte(rawStr), &item); err != nil {
			return nil, err
		}
		byID[item.ID] = item
	}

	if len(missIDs) > 0 {
		var rows []domain.Item
		if err := r.sqldb.WithContext(ctx).Where("id IN ?", missIDs).Find(&rows).Error; err != nil {
			return nil, err
		}

		// kv := make([]any, 0, len(rows)*2)
		pipe := r.redisdb.Pipeline()
		for _, item := range rows {
			byID[item.ID] = item
			buf, err := json.Marshal(&item)
			if err != nil {
				continue
			}
			// kv = append(kv, domain.KeyItemIDToItem(item.ID), string(buf))
			pipe.Set(ctx, domain.KeyItemIDToItem(item.ID), string(buf), r.itemTTL)
		}
		pipe.Exec(ctx)
		// if len(kv) > 0 {
		// 	if err := r.redisdb.MSet(ctx, kv...).Err(); err != nil { // TODO: 添加缓存过期时间（pipline方法/reids8.4+方法）
		// 		return nil, err
		// 	}
		// }
	}

	items := make([]domain.Item, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// recsUpdateScript 原子更新"最近推荐"zset：
// 1) 现有成员 score 全部 +1（老化一轮）
// 2) 删除 score >= 窗口阈值的成员（出窗回归）
// 3) 本次新推荐的 item 以 score=0 插入（重新计时）
var recsUpdateScript = redis.NewScript(`
local members = redis.call('ZRANGE', KEYS[1], 0, -1)
for _, m in ipairs(members) do
  redis.call('ZINCRBY', KEYS[1], 1, m)
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], ARGV[1], '+inf')
for i = 2, #ARGV do
  redis.call('ZADD', KEYS[1], 0, ARGV[i])
end
return 1
`)

// RecordRecs 记录本次推荐结果到"最近推荐"zset。
func (r *ItemRepository) RecordRecs(ctx context.Context, userID uint, items []domain.Item) error {
	key := domain.RecentlyRecommended(userID)
	args := make([]any, 0, len(items)+1)
	args = append(args, r.recsWindow)
	for _, it := range items {
		args = append(args, strconv.FormatUint(uint64(it.ID), 10))
	}
	return recsUpdateScript.Run(ctx, r.redisdb, []string{key}, args...).Err()
}
