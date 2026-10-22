package mylog

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

// AsynqLogger 适配 asynq.Logger 接口
type AsynqLogger struct {
	zl *zerolog.Logger
}

func NewAsynqLogger(zl *zerolog.Logger) AsynqLogger {
	return AsynqLogger{
		zl: zl,
	}
}

func (l *AsynqLogger) Debugf(format string, args ...interface{}) {
	l.zl.Debug().Msgf(format, args...)
}

func (l *AsynqLogger) Infof(format string, args ...interface{}) {
	l.zl.Info().Msgf(format, args...)
}

func (l *AsynqLogger) Warnf(format string, args ...interface{}) {
	l.zl.Warn().Msgf(format, args...)
}

func (l *AsynqLogger) Errorf(format string, args ...interface{}) {
	l.zl.Error().Msgf(format, args...)
}

func (l *AsynqLogger) Fatalf(format string, args ...interface{}) {
	l.zl.Fatal().Msgf(format, args...)
}

func TraceMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		// 从task header读取trace_id
		requestID, ok := task.Headers()["request_id"]
		if requestID == "" || !ok {
			// 如果上游没有传递trace，新建一个（兜底）
			requestID = "unknown"
		}
		// 把traceID存入context，使用自定义key避免冲突
		ctx = context.WithValue(ctx, "X-Request-ID", requestID)

		err := next.ProcessTask(ctx, task)

		// 任务正常结束日志
		if err == nil {
			GlobalLogger.Info().
				Str("request_id", requestID).
				Str("task_type", task.Type()).
				Msg("asynq task complete")
		}
		return err
	})
}

func RequestIDFromCtx(ctx context.Context) string {
	val, ok := ctx.Value("X-Request-ID").(string)
	if !ok || val == "" {
		return "unknown"
	}
	return val
}