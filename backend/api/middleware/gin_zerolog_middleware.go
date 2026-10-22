package middleware

import (
	"time"

	"litstream/pkg/mylog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ZerologGinMiddleware gin请求日志中间件，接收自定义zerolog.Logger
func ZerologGinMiddleware(zl *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 忽略/hello路径
		if c.Request.URL.Path == "/hello" {
			c.Next()
			return
		}
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		method := c.Request.Method
		clientIP := c.ClientIP()
		// 从请求Header读取上游传递的链路ID
		requestID := c.GetHeader("X-Request-ID")
		// 判断：如果上游没有传递（为空）
		if requestID == "" {
			// 兜底：当前Gin中间件生成新的UUID作为本次请求ID
			requestID = uuid.NewString()
		}
		// 把最终确定的requestId写入响应Header返回给调用方（前端/网关可以拿到这个ID）
		c.Header("X-Request-ID", requestID)

		// 把logger注入到gin上下文，后续handler可以直接取使用
		requestLogger := zl.With().
			Str("request_id", requestID). // 从header取requestId，也可以自己生成
			Str("method", method).
			Str("path", path).
			Str("client_ip", clientIP).
			Logger()
		c.Set(mylog.CtxGLKey{}, &requestLogger)

		// 执行后续handler
		c.Next()

		// handler执行完成后，拿到响应状态码
		statusCode := c.Writer.Status()
		latency := time.Since(start)

		// 拼接query
		if raw != "" {
			path = path + "?" + raw
		}

		// 根据状态码选择日志级别
		event := requestLogger.Info()
		switch {
		case statusCode >= 500:
			event = requestLogger.Error()
		case statusCode >= 400:
			event = requestLogger.Warn()
		}

		// 输出请求日志
		event.
			Int("status_code", statusCode).
			Dur("latency", latency).
			Str("path_full", path).
			Msg("http request")
	}
}
