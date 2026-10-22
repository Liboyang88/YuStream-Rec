package mylog

import (
	"context"
	"time"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GormLogger 实现 gorm logger.Interface
type GormLogger struct {
	logKey any
	slowThreshold time.Duration
	ignoreNotFound bool
	logLevel logger.LogLevel
}

func NewGormLogger(
	logKey any, slowThreshold time.Duration, ignoreNotFound bool, level logger.LogLevel) *GormLogger {
	return &GormLogger{
		logKey: logKey,
		slowThreshold: slowThreshold,
		ignoreNotFound: ignoreNotFound,
		logLevel: level,
	}
}

func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	newL := *l
	newL.logLevel = level
	return &newL
}

func (l *GormLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	if l.logLevel >= logger.Info {
		LoggerOf(ctx, l.logKey).Info().Msgf(msg, args...)
	}
}

func (l *GormLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	if l.logLevel >= logger.Warn {
		LoggerOf(ctx, l.logKey).Warn().Msgf(msg, args...)
	}
}

func (l *GormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	if l.logLevel >= logger.Error {
		LoggerOf(ctx, l.logKey).Error().Msgf(msg, args...)
	}
}

// Trace 核心，打印SQL耗时
func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	logEvent := LoggerOf(ctx, l.logKey)
	switch {
	case err != nil:
		if err == gorm.ErrRecordNotFound && l.ignoreNotFound {
			return
		}
		logEvent.Error().
			Err(err).
			Str("sql", sql).
			Int64("rows", rows).
			Dur("elapsed_ms", elapsed).
			Msg("gorm sql error")
	case elapsed > l.slowThreshold && l.slowThreshold != 0:
		logEvent.Warn().
			Str("sql", sql).
			Int64("rows", rows).
			Dur("elapsed_ms", elapsed).
			Msg("gorm slow sql")
	case l.logLevel >= logger.Info:
		logEvent.Info().
			Str("sql", sql).
			Int64("rows", rows).
			Dur("elapsed_ms", elapsed).
			Msg("gorm sql")
	}
}