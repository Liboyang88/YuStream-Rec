package worker

import (
	"context"
	"dcpro/domain"
	"dcpro/internal/embedutil"
	"dcpro/llm"
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type MyWorker struct {
	sqldb         *gorm.DB
	redisdb       *redis.Client
	llmInvoking   *llm.LLMInvoking
	embedder      *embedutil.Embedding
	expiration    time.Duration //24h
	ia_expiration time.Duration //14Day
	maxLen        int
}

func NewMyWorker(sqldb *gorm.DB, redisdb *redis.Client,
	llmInvoking *llm.LLMInvoking,
	embedder *embedutil.Embedding,
	expirationHour int, iaeDay int, maxLen int) *MyWorker {

	return &MyWorker{sqldb: sqldb,
		redisdb:       redisdb,
		llmInvoking:   llmInvoking,
		embedder:      embedder,
		expiration:    time.Duration(expirationHour) * time.Hour,
		ia_expiration: time.Duration(iaeDay) * 24 * time.Hour,
		maxLen:        maxLen}
}

func (sw *MyWorker) HandleUpdateUserInfo(ctx context.Context, task *asynq.Task) error {
	var payload domain.UpdateUserInfoPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	if err := sw.sqldb.WithContext(ctx).Model(&domain.User{}).Where("id = ?", payload.ID).Updates(payload.UpdateFields).Error; err != nil {
		return err
	}
	return nil
}

// HandleAddNewUser 已废弃：注册改为同步插入（方案A），此任务不再入队。
// 保留以兼容旧队列中残留的任务。
func (sw *MyWorker) HandleAddNewUser(ctx context.Context, task *asynq.Task) error {
	var payload domain.AddNewUserPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}

	if err := sw.sqldb.WithContext(ctx).Create(&domain.User{
		Username: payload.Username,
		Password: payload.Password,
	}).Error; err != nil {
		return err
	}
	return nil
}

func (sw *MyWorker) HandleUpdateUS(ctx context.Context, task *asynq.Task) error {
	var payload domain.UpdateUSPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	uid := payload.UserID

	// 1. 加载用户：Redis -> MySQL 回退
	user := &domain.User{}
	mapStrStr, err := sw.redisdb.HGetAll(ctx, domain.KeyUserIDToInfo(uid)).Result()
	if err != nil {
		return err
	}
	if len(mapStrStr) > 0 {
		if err := domain.HashToUser(mapStrStr, user); err != nil {
			return err
		}
	}
	if user.ID == 0 {
		if err := sw.sqldb.WithContext(ctx).First(user, uid).Error; err != nil {
			return err
		}
		hash, err := domain.UserToHash(user)
		if err != nil {
			return err
		}
		if err := sw.redisdb.HSet(ctx, domain.KeyUserIDToInfo(uid), hash).Err(); err != nil {
			return err
		}
		if err := sw.redisdb.Expire(ctx, domain.KeyUserIDToInfo(uid), sw.expiration).Err(); err != nil {
			return err
		}
	}

	// 2. 拆分交互（new / his）
	msgs, err := sw.redisdb.XRevRange(ctx, domain.KeyUserIDToInteractions(uid), "+", "-").Result()
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	newIA := make([]domain.Interaction, 0)
	hisIA := make([]domain.Interaction, 0)

	ptr, err := sw.redisdb.Get(ctx, domain.KeyUserIDToIaPtr(uid)).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	before := ptr != "" // 首次更新（无指针）时，全部视为 new
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

	// 3. 更新交互指针
	if err := sw.redisdb.Set(ctx, domain.KeyUserIDToIaPtr(uid), msgs[0].ID, sw.ia_expiration).Err(); err != nil {
		return err
	}

	// 4. LLM 更新画像
	if err := sw.llmInvoking.UpdateUserProfile(user, &newIA, &hisIA); err != nil {
		return err
	}

	// 5. 写回 Redis + MySQL（错误分开判断，不互相覆盖）
	if err := sw.redisdb.HSet(ctx, domain.KeyUserIDToInfo(uid), "summary", user.Summary).Err(); err != nil {
		return err
	}
	if err := sw.redisdb.Expire(ctx, domain.KeyUserIDToInfo(uid), sw.expiration).Err(); err != nil {
		return err
	}
	if err := sw.sqldb.WithContext(ctx).Model(&domain.User{}).Where("id = ?", uid).Update("summary", user.Summary).Error; err != nil {
		return err
	}

	return nil
}

func (sw *MyWorker) HandleGenerateUS(ctx context.Context, task *asynq.Task) error {
	var payload domain.GenerateUSPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}

	user := &domain.User{}
	mapStrStr, err := sw.redisdb.HGetAll(ctx, domain.KeyUserIDToInfo(payload.UserID)).Result()
	if err != nil {
		return err
	}
	if len(mapStrStr) > 0 {
		if err := domain.HashToUser(mapStrStr, user); err != nil {
			return err
		}
	}
	if user.ID == 0 {
		if err := sw.sqldb.WithContext(ctx).First(user, payload.UserID).Error; err != nil {
			return err
		}
	}

	if user.ID == 0 {
		return errors.New("user not exist")
	}

	// LLM 调用错误必须接住，否则会把空 summary 写库
	if err := sw.llmInvoking.GenerateUserProfile(user); err != nil {
		return err
	}

	if err := sw.redisdb.HSet(ctx, domain.KeyUserIDToInfo(payload.UserID), "summary", user.Summary).Err(); err != nil {
		return err
	}
	if err := sw.redisdb.Expire(ctx, domain.KeyUserIDToInfo(payload.UserID), sw.expiration).Err(); err != nil {
		return err
	}

	if err := sw.sqldb.WithContext(ctx).Model(&domain.User{}).Where("id = ?", payload.UserID).Update("summary", user.Summary).Error; err != nil {
		return err
	}
	return nil
}

func (sw *MyWorker) AddNewInteraction(ctx context.Context, task *asynq.Task) error {
	var payload domain.AddNewInteractionPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}

	if err := sw.redisdb.XAdd(ctx, &redis.XAddArgs{
		Stream: domain.KeyUserIDToInteractions(payload.UserID),
		MaxLen: int64(sw.maxLen),
		Values: map[string]any{"payload": payload.Payload}}).Err(); err != nil {
		return err
	}
	if err := sw.redisdb.Expire(ctx, domain.KeyUserIDToInteractions(payload.UserID), sw.ia_expiration).Err(); err != nil {
		return err
	}
	return nil
}

func (sw *MyWorker) getUserSummary(ctx context.Context, id uint) (string, error) {
	summary, err := sw.redisdb.HGet(ctx, domain.KeyUserIDToInfo(id), "summary").Result();
	if err != nil && err != redis.Nil {
		return "", err
	}
	if summary != ""{
		return summary, nil
	}

	user := &domain.User{}

	if err := sw.sqldb.WithContext(ctx).First(user, id).Error;err!= nil{
		return "", err
	}
	return user.Summary, nil
}

func 