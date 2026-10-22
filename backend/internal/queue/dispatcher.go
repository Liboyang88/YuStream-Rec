package queue

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
)

// Dispatcher 是异步任务的唯一投递口（producer 侧）。
// service 需要异步执行某业务时，只允许通过 Dispatcher 入队，
// 不允许任何业务代码直接操作 asynq。
type Dispatcher struct {
	client *asynq.Client
	uniqueTTL1 time.Duration
	uniqueTTL2 time.Duration
}

// NewDispatcher 用 asynq 客户端构造投递器。
func NewDispatcher(client *asynq.Client, uniqueTTL1, uniqueTTL2 time.Duration) *Dispatcher {
	return &Dispatcher{client: client, uniqueTTL1: uniqueTTL1, uniqueTTL2: uniqueTTL2}
}

// EnqueueIncrementalProfileUpdate 投递"增量更新用户画像"任务。
// 注意：只负责入队并立即返回；真正的执行业务逻辑在 UserService.IncrementalProfileUpdate。
func (d *Dispatcher) EnqueueIncrementalProfileUpdate(userID uint, requestID string) error {
	task, err := newUpdateUSTask(userID, requestID)
	if err != nil {
		return err
	}
	_, err = d.client.Enqueue(task, asynq.MaxRetry(3), asynq.Unique(d.uniqueTTL1))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil // 已有一个在途/排队任务 → 本次合并掉,不是失败
	}
	return err
}

// EnqueueProfileRegeneration 投递"重新生成用户画像"任务。
// 注意：只负责入队并立即返回；真正的执行业务逻辑在 UserService.RegenerateProfile。
func (d *Dispatcher) EnqueueProfileRegeneration(userID uint, requestID string) error {
	task, err := newGenerateUSTask(userID, requestID)
	if err != nil {
		return err
	}
	_, err = d.client.Enqueue(task, asynq.MaxRetry(3), asynq.Unique(d.uniqueTTL1))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil // 已有一个在途/排队任务 → 本次合并掉,不是失败
	}
	return err
}

// EnqueueRecommendation 投递"精排推荐"任务，精排结果写入 resultKey。
// LLM 慢，失败由 asynq 重试（MaxRetry 3）；只负责入队并立即返回，
// 真正的执行业务逻辑在 ItemService.GenerateRecommendation。
func (d *Dispatcher) EnqueueRecommendation(userID uint, requestID string) error {
	task, err := newGenerateRecTask(userID, requestID)
	if err != nil {
		return err
	}
	_, err = d.client.Enqueue(task, asynq.MaxRetry(3), asynq.Unique(d.uniqueTTL2))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil // 已有一个在途/排队任务 → 本次合并掉,不是失败
	}
	return err
}

func (d *Dispatcher) EnqueueUpdateUSWithNewInfo(userID uint, cols map[string]any, requestID string) error {
	colsJson, err := json.Marshal(cols)
	if err != nil {
		return err
	}
	colsToStr := string(colsJson)
	task, err := newUpdateUSWithNewInfoTask(userID, colsToStr, requestID)
	if err != nil {
		return err
	}
	_, err = d.client.Enqueue(task, asynq.MaxRetry(3), asynq.Unique(d.uniqueTTL1))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil // 已有一个在途/排队任务 → 本次合并掉,不是失败
	}
	return err
}