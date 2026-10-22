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

// ItemWorker 持有推荐服务的执行体（bootstrap 注入），供推荐任务 handler 调用。
type ItemWorker struct {
	items *service.ItemService
}

// NewItemWorker 构造推荐任务处理器。
func NewItemWorker(items *service.ItemService) *ItemWorker {
	return &ItemWorker{items: items}
}

// HandleGenerateRecommendation TypeGenerateRecommendation 任务：
// 拆单 → 调 ItemService.GenerateRecommendation（精排并把结果写入 resultKey）。
func (w *ItemWorker) HandleGenerateRecommendation(ctx context.Context, task *asynq.Task) error {
	var payload queue.GenerateRecPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return w.items.GenerateRecommendation(ctx, payload.UserID)
}

// RegisterItemTasks 注册推荐域任务类型 → handler 的映射（bootstrap 组装 mux 时调用）。
func RegisterItemTasks(mux *asynq.ServeMux, w *ItemWorker) {
	mux.HandleFunc(queue.TypeGenerateRecommendation, w.HandleGenerateRecommendation)
}
