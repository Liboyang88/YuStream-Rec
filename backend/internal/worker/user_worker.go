// Package worker 只做一件事：把 asynq 任务"翻译"成对 service 的调用。
// 这里不允许出现任何业务逻辑/存取逻辑——业务唯一实现在 internal/service。
package worker

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"

	"litstream/internal/queue"
	"litstream/internal/service"
)

// UserWorker 持有用户服务的执行体（bootstrap 注入），供各任务 handler 调用。
type UserWorker struct {
	svc *service.UserService
}

// NewUserWorker 构造用户任务处理器。
func NewUserWorker(svc *service.UserService) *UserWorker {
	return &UserWorker{svc: svc}
}

// HandleUpdateUS TypeUpdateUS 任务：拆单 → 调 UserService.IncrementalProfileUpdate。
func (w *UserWorker) HandleUpdateUS(ctx context.Context, task *asynq.Task) error {
	var payload queue.UpdateUSPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return w.svc.IncrementalProfileUpdate(ctx, payload.UserID)
}

// HandleGenerateUS TypeGenerateUS 任务：拆单 → 调 UserService.RegenerateProfile。
func (w *UserWorker) HandleGenerateUS(ctx context.Context, task *asynq.Task) error {
	var payload queue.GenerateUSPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return w.svc.GenerateProfile(ctx, payload.UserID)
}

func (w *UserWorker) HandleUpdateUSWithNewInfo(ctx context.Context, task *asynq.Task) error {
	var payload queue.UpdateUSWithNewInfoPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return w.svc.ReProfileWithNewInfo(ctx, payload.UserID, payload.Cols)
}

// RegisterUserTasks 注册用户域任务类型 → handler 的映射（bootstrap 组装 mux 时调用）。
func RegisterUserTasks(mux *asynq.ServeMux, w *UserWorker) {
	mux.HandleFunc(queue.TypeUpdateUS, w.HandleUpdateUS)
	mux.HandleFunc(queue.TypeGenerateUS, w.HandleGenerateUS)
	mux.HandleFunc(queue.TypeUpdateUSWithNewInfo, w.HandleUpdateUSWithNewInfo)
}
