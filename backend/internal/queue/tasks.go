// Package queue 是异步任务（基于 asynq + Redis）的统一收口：
// 任务类型常量、payload、任务构造、投递入口（Dispatcher）都在这里。
// 业务层（service）只通过 Dispatcher 投递任务；domain 不再依赖 asynq。
package queue

import (
	"encoding/json"

	"github.com/hibiken/asynq"
)

const (
	// TypeUpdateUS 增量更新用户画像（互动产生后，由 LLM 慢速执行）
	TypeUpdateUS = "user_summary:update"
	// TypeGenerateUS 重新生成用户画像（手动触发，由 LLM 慢速执行）
	TypeGenerateUS = "user_summary:generate"
	// TypeGenerateRecommendation 精排推荐（异步生成，结果写入 payload.ResultKey）
	TypeGenerateRecommendation = "recommendation:generate"
	TypeUpdateUSWithNewInfo = "user_summary:new_info"
)

// UpdateUSPayload 增量更新用户画像任务的载荷
type UpdateUSPayload struct {
	UserID uint `json:"user_id"`
}

// GenerateUSPayload 重新生成用户画像任务的载荷
type GenerateUSPayload struct {
	UserID uint `json:"user_id"`
}

// GenerateRecPayload 精排推荐任务的载荷。
// ResultKey 是写入结果的随机 key（前端稍后凭它取精排结果）。
type GenerateRecPayload struct {
	UserID    uint   `json:"user_id"`
}

type UpdateUSWithNewInfoPayload struct {
	UserID uint `json:"user_id"`
	Cols   string `json:"cols"`
}

func newUpdateUSTask(userID uint, requestID string) (*asynq.Task, error) {
	payload, err := json.Marshal(UpdateUSPayload{UserID: userID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeUpdateUS, payload, map[string]string{"request_id": requestID}), nil
}

func newGenerateUSTask(userID uint, requestID string) (*asynq.Task, error) {
	payload, err := json.Marshal(GenerateUSPayload{UserID: userID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeGenerateUS, payload, map[string]string{"request_id": requestID}), nil
}

func newGenerateRecTask(userID uint, requestID string) (*asynq.Task, error) {
	payload, err := json.Marshal(GenerateRecPayload{UserID: userID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeGenerateRecommendation, payload, map[string]string{"request_id": requestID}), nil
}

func newUpdateUSWithNewInfoTask(userID uint, cols string, requestID string) (*asynq.Task, error) {
	payload, err := json.Marshal(UpdateUSWithNewInfoPayload{UserID: userID, Cols: cols})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeUpdateUSWithNewInfo, payload, map[string]string{"request_id": requestID}), nil
}