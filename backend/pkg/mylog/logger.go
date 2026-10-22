package mylog

import (
	"context"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/rs/zerolog"
)

var GlobalLogger *zerolog.Logger

func NewLogger(filename string, level zerolog.Level) *zerolog.Logger {
	rotatedFileWriter := &lumberjack.Logger{
		Filename:   filename, // 当前日志文件路径；轮转后自动生成 app-2026-09-14Txxxx.log
		MaxSize:    10,               // 单个文件最大10MB，超过自动切割
		MaxBackups: 20,               // 最多保留20个备份文件
		MaxAge:     7,                // 日志保留7天，到期自动删除
		Compress:   true,             // 旧日志gzip压缩
		LocalTime:  true,             // 使用本地时间命名分片文件（日期分片核心）
	}

	// ========== 2. 控制台输出 ==========
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// ========== 3. MultiLevelWriter 双写：控制台 + 轮转文件 ==========
	multiWriter := zerolog.MultiLevelWriter(consoleWriter, rotatedFileWriter)

	// ========== 4. 创建自定义Logger ==========
	l := zerolog.New(multiWriter).With().
		Timestamp().
		Str("service", "order-service").
		Str("env", "prod").
		Caller(). // 带上代码行号
		Logger().
		Level(level)
		
	return &l
}

func InitLogger(level string) {
	var thisLevel zerolog.Level
	switch level {
	case "error":
		thisLevel = zerolog.ErrorLevel
	case "warn":
		thisLevel = zerolog.WarnLevel
	case "info":
		thisLevel = zerolog.InfoLevel
	case "debug":
		thisLevel = zerolog.DebugLevel
	case "trace":
		thisLevel = zerolog.TraceLevel
	default:
		thisLevel = zerolog.WarnLevel
	}
	GlobalLogger = NewLogger("./logs/app.log", thisLevel)
}


type CtxGLKey struct{}

func LoggerOf(ctx context.Context, key any) *zerolog.Logger {
	if l, ok := ctx.Value(key).(*zerolog.Logger); ok && l != nil {
		return l
	}
	return GlobalLogger // 兜底:worker / 后台路径用全局logger
}