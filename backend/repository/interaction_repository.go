package repository

import (
	"context"
	"dcpro/domain"
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type InteractionRepository struct {
	expiration time.Duration //24h
	ia_expiration time.Duration //14Day
	maxLen int //保存的最近的interaction数据最近几条
	redisdb *redis.Client
	qw *asynq.Client
}

func NewInteractionRepository(redisdb *redis.Client, qw *asynq.Client, expirationHour int, iaeDay int, maxLen int) *InteractionRepository {
	return &InteractionRepository{redisdb: redisdb, qw: qw, 
		expiration: time.Duration(expirationHour) * time.Hour,
		ia_expiration: time.Duration(iaeDay) * time.Hour*24,
		maxLen: maxLen}
}

func (inr *InteractionRepository) CreateInteraction(ctx context.Context, interaction domain.Interaction, userID uint) error {
	buf, err := json.Marshal(interaction)
	if err != nil{ return err}

	err = inr.redisdb.XAdd(ctx, &redis.XAddArgs{
		Stream: domain.KeyUserIDToInteractions(userID),
		MaxLen: int64(inr.maxLen),
		Values: map[string]any{"payload":string(buf)}}).Err()
	err = inr.redisdb.Expire(ctx, domain.KeyUserIDToInteractions(userID), inr.ia_expiration).Err()
	if err != nil { return err}
	return nil
}

func (inr *InteractionRepository) AddNewInteraction(ctx context.Context, interaction domain.Interaction, userID uint) error {
	buf, err := json.Marshal(interaction)
	if err != nil{ return err}
	task, err := domain.NewAddNewInteractionTask(userID, string(buf))
	_, err = inr.qw.Enqueue(task)
	if err != nil { return err}
	return nil
}


func (inr *InteractionRepository) GetInteractionsByUserID(ctx context.Context, interactions *[]domain.Interaction, userID uint) error { 
	*interactions = make([]domain.Interaction, 0)
	msgs ,err := inr.redisdb.XRevRange(ctx, domain.KeyUserIDToInteractions(userID), "+", "-").Result()
	if err != nil { return err}
	
	for _, msg := range msgs { 
		jsonStr, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var interaction domain.Interaction
		if err := json.Unmarshal([]byte(jsonStr), &interaction); err != nil {
			continue
		}
		*interactions = append(*interactions, interaction)
	}
	return nil
}

func (inr *InteractionRepository) GetIaByUserID(ctx context.Context, new_ia *[]domain.Interaction, his_ia *[]domain.Interaction, userID uint) (string, error) { 
	*new_ia = make([]domain.Interaction, 0)
	*his_ia = make([]domain.Interaction, 0)
	msgs ,err := inr.redisdb.XRevRange(ctx, domain.KeyUserIDToInteractions(userID), "+", "-").Result()
	if err != nil { return "",err}
	if len(msgs) == 0 {
		return "", nil
	}

	ptr, err := inr.redisdb.Get(ctx, domain.KeyUserIDToIaPtr(userID)).Result()
	if err != nil && err != redis.Nil {
		return "", err
	}
	before := ptr != "" // 首次（无指针）时全部视为 new
	for _, msg := range msgs {
		jsonStr, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var interaction domain.Interaction
		if err := json.Unmarshal([]byte(jsonStr), &interaction); err != nil {
			continue
		}
		if before && msg.ID == ptr {
			before = false
		}
		if before {
			*new_ia = append(*new_ia, interaction)
		} else {
			*his_ia = append(*his_ia, interaction)
		}
	}
	return msgs[0].ID,nil
}

func (inr *InteractionRepository) UpdateIaPtr(ctx context.Context, userID uint, ptr string) error { 
	if err := inr.redisdb.Set(ctx, domain.KeyUserIDToIaPtr(userID), ptr, inr.ia_expiration).Err(); err != nil { 
		return err
	}
	return nil
}