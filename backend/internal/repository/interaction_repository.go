package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"litstream/domain"

	"github.com/redis/go-redis/v9"
)

// InteractionRepository 互动数据访问：每个用户的互动保存在一条 Redis Stream 中，
// 并提供"按指针切分 new/his"的读取能力（供画像增量更新业务复用，实现唯一化）。
type InteractionRepository struct {
	iaTTL   time.Duration // stream/指针的 TTL（14 天）
	maxLen  int           // 每条 stream 保留的最大消息数
	redisdb *redis.Client
}

// NewInteractionRepository 构造互动仓储。
// iaExpireDay: 互动数据保留天数（对应 env.RedisIAExpireTime）；maxLen 对应 env.RedisMaxLen。
func NewInteractionRepository(redisdb *redis.Client, iaTTL time.Duration, maxLen int) *InteractionRepository {
	return &InteractionRepository{
		iaTTL:   iaTTL,
		maxLen:  maxLen,
		redisdb: redisdb,
	}
}

// Append 同步写入一条互动到用户 stream（XAdd + 整流续期）。毫秒级操作，由同步业务调用。
func (r *InteractionRepository) Append(ctx context.Context, userID uint, ia domain.Interaction) error {
	buf, err := json.Marshal(ia)
	if err != nil {
		return err
	}
	if err := r.redisdb.XAdd(ctx, &redis.XAddArgs{
		Stream: domain.KeyUserIDToInteractions(userID),
		MaxLen: int64(r.maxLen),
		Values: map[string]any{"payload": string(buf)},
	}).Err(); err != nil {
		return err
	}
	return r.redisdb.Expire(ctx, domain.KeyUserIDToInteractions(userID), r.iaTTL).Err()
}

// FetchDelta 读取用户互动并按指针切分为"本轮新增(new)"与"历史(his)"。
// latestID 为 stream 最新消息 ID（无消息时为空串）。本方法不推进指针，
// 由调用方在业务成功后调用 AdvancePointer，避免 LLM 失败时数据被跳过。
func (r *InteractionRepository) FetchDelta(ctx context.Context, userID uint) (newIA, hisIA []domain.Interaction, latestID string, err error) {
	newIA = make([]domain.Interaction, 0)
	hisIA = make([]domain.Interaction, 0)

	msgs, err := r.redisdb.XRevRange(ctx, domain.KeyUserIDToInteractions(userID), "+", "-").Result()
	if err != nil {
		return nil, nil, "", err
	}
	if len(msgs) == 0 {
		return newIA, hisIA, "", nil
	}

	ptr, err := r.redisdb.Get(ctx, domain.KeyUserIDToIaPtr(userID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, nil, "", err
	}

	before := ptr != "" // 首次（无指针）时全部视为 new
	for _, msg := range msgs {
		jsonStr, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var ia domain.Interaction
		if err := json.Unmarshal([]byte(jsonStr), &ia); err != nil {
			continue
		}
		if before && msg.ID == ptr {
			before = false
		}
		if before {
			newIA = append(newIA, ia)
		} else {
			hisIA = append(hisIA, ia)
		}
	}
	return newIA, hisIA, msgs[0].ID, nil
}

// AdvancePointer 把用户的画像增量指针推进到 latestID（业务成功后才允许推进）。
func (r *InteractionRepository) AdvancePointer(ctx context.Context, userID uint, latestID string) error {
	return r.redisdb.Set(ctx, domain.KeyUserIDToIaPtr(userID), latestID, r.iaTTL).Err()
}
