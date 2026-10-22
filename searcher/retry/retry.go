// Package retry 提供带退避的简单重试，供外部接口调用（文献介绍生成、翻译）复用。
package retry

import (
	"context"
	"errors"
	"log"
	"time"
)

// Config 控制重试策略。
type Config struct {
	// MaxRetries 是首次失败后的额外重试次数：总调用次数最多为 1+MaxRetries。
	MaxRetries int
	// Backoff 是重试等待的基数，按第几次重试线性增长（Backoff、2*Backoff、…）。
	Backoff time.Duration
}

// DefaultConfig 是默认策略：失败后再重试 3 次（最多 4 次调用），退避 2s / 4s / 6s。
func DefaultConfig() Config {
	return Config{MaxRetries: 3, Backoff: 2 * time.Second}
}

// permanentError 标记"重试也没用"的错误（例如 key 无效、模型名不存在）。
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent 把 err 标记为不可重试：Do 遇到它会立刻返回，不再等待与重试。
// 用于区分"网络抖动"和"配置错了"——后者重试只会白等、白花调用次数。
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent 判断 err（或其包装链上）是否被标记为不可重试。
func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

// Do 执行 fn；失败时按 cfg 重试，返回最后一次的错误。
//
// ctx 取消、或错误被 Permanent 标记时不再重试，立即返回该错误。
func Do(ctx context.Context, cfg Config, what string, fn func() error) error {
	attempts := cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			wait := time.Duration(attempt-1) * cfg.Backoff
			log.Printf("%s 第 %d/%d 次尝试失败，%s 后重试: %v", what, attempt-1, attempts, wait, lastErr)
			if err := sleep(ctx, wait); err != nil {
				return lastErr
			}
		}

		if lastErr = fn(); lastErr == nil {
			return nil
		}
		if IsPermanent(lastErr) || ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

// sleep 等待 d，期间响应 ctx 取消。
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
