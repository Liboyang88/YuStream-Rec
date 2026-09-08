package repository

import (
	"context"
	"strconv"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"dcpro/db"
	"dcpro/domain"
	"dcpro/internal/embedutil"
	"dcpro/llm"

	"encoding/json"
)

type ItemRepository struct {
	limitTopK int
	recsWindow int
	sqldb *gorm.DB
	milvus *db.MilvusDB
	redisdb *redis.Client
	qw *asynq.Client
	embedding *embedutil.Embedding
	llmRecommender *llm.LLMInvoking
}

func NewItemRepository(limitTopK int, recsWindow int, sqldb *gorm.DB, milvus *db.MilvusDB, redisdb *redis.Client, qw *asynq.Client, embedding *embedutil.Embedding, llmRecommender *llm.LLMInvoking) *ItemRepository {
	return &ItemRepository{
		limitTopK: limitTopK,
		recsWindow: recsWindow,
		sqldb: sqldb,
		milvus: milvus,
		redisdb: redisdb,
		qw: qw,
		embedding: embedding,
		llmRecommender: llmRecommender,
	}
}

func (ir *ItemRepository) GetItemsByIds(ctx context.Context, userID uint, userSummary string, items *[]domain.Item) error {
	vec, err := ir.embedding.Encode(userSummary)
	if err != nil {
		return err
	}

	excludeIDs, err := ir.recExcludedIDs(ctx, userID)
	if err != nil {
		return err
	}
	
	idList, err := ir.milvus.SearchVec(ctx, &vec, ir.limitTopK, excludeIDs)
	if err != nil {
		return err
	}
	if len(idList) > ir.limitTopK {
		idList = idList[:ir.limitTopK]
	}
	// 如果不足 再去掉过滤条件再检索
	if len(idList) < ir.limitTopK {
		more, err := ir.milvus.SearchVec(ctx, &vec, ir.limitTopK, nil)
		if err != nil {
			return err
		}
		seen := make(map[uint]struct{}, len(idList))
		for _, id := range idList {
			seen[id] = struct{}{}
		}
		for _, id := range more {
			if _, ok := seen[id]; ok {
				continue
			}
			idList = append(idList, id)
			seen[id] = struct{}{}
			if len(idList) >= ir.limitTopK {
				break
			}
		}
	}
	// 无检索结果：直接返回空，避免下面 Find 无条件下查出全表
	if len(idList) == 0 {
		*items = nil
		return nil
	}

	cacheKeys := make([]string, 0, len(idList))
	var hitItems []domain.Item
	var missIds []uint

	for _, id := range idList {
		cacheKeys = append(cacheKeys, domain.KeyItemIDToItem(id))
	}
	cacheValues, err := ir.redisdb.MGet(ctx, cacheKeys...).Result()
	if err != nil {
		return err
	}

	for ind, v := range cacheValues {
		rawStr, ok := v.(string) // MGet 缺失 key 返回 nil，必须先类型断言
		if !ok || rawStr == "" {
			missIds = append(missIds, idList[ind])
			continue
		}
		var item domain.Item
		if err := json.Unmarshal([]byte(rawStr), &item); err != nil {
			return err
		}
		hitItems = append(hitItems, item)
	}

	if len(missIds) > 0 {
		if err := ir.sqldb.WithContext(ctx).Where("id IN ?", missIds).Find(items).Error; err != nil {
			return err
		}
		// 回填缓存
		newKV := make([]interface{}, 0, 2*len(missIds))
		for _, item := range *items {
			buf, err := json.Marshal(&item)
			if err != nil {
				return err
			}
			newKV = append(newKV, domain.KeyItemIDToItem(item.ID), string(buf))
		}
		if err := ir.redisdb.MSet(ctx, newKV...).Err(); err != nil {
			return err
		}
	}

	*items = append(*items, hitItems...)

	err = ir.llmRecommender.Recommend(userID, userSummary, items, len(idList), ir.limitTopK)
	if err != nil {
		return err
	}

	if err := ir.updateRecs(ctx, userID, *items); err != nil {
		return err
	}

	return nil
}

// 读取排除集：score 0..9 的成员 = 最近 10 次推荐内出现过的 item
func (ir *ItemRepository) recExcludedIDs(ctx context.Context, userID uint) ([]uint, error) {
	key := domain.RecentlyRecommended(userID)
	members, err := ir.redisdb.ZRangeByScore(ctx, key,
		&redis.ZRangeBy{Min: "0", Max: strconv.Itoa(ir.recsWindow - 1)}).Result()
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

// recsUpdateScript：原子更新最近推荐 ZSET
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

func (ir *ItemRepository) updateRecs(ctx context.Context, userID uint, items []domain.Item) error {
	key := domain.RecentlyRecommended(userID)
	args := make([]interface{}, 0, len(items)+1)
	args = append(args, ir.recsWindow)
	for _, it := range items {
		args = append(args, strconv.FormatUint(uint64(it.ID), 10))
	}
	return recsUpdateScript.Run(ctx, ir.redisdb, []string{key}, args...).Err()
}