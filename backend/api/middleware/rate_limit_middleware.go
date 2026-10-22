package middleware

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"litstream/pkg"
	"litstream/pkg/mylog"
)

var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)

type RateLimiter struct {
	rdb    *redis.Client
	prefix string
	limit  int64
	window time.Duration
}

func NewRateLimiter(rdb *redis.Client, prefix string, limit int64, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, prefix: prefix, limit: limit, window: window}
}

// Limit 按 keyFn 给出的维度限流；超限返回 429 + Retry-After。
// 限流器自身故障时 fail-open（不阻断业务），但会打日志。
func (l *RateLimiter) Limit(keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := l.prefix + ":" + keyFn(c)
		n, err := rateLimitScript.Run(c, l.rdb, []string{key}, l.window.Milliseconds()).Int64()
		if err != nil {
			mylog.LoggerOf(c, mylog.CtxGLKey{}).Error().Err(err).Str("key", key).Msg("rate limiter failed, fail-open")
			c.Next()
			return
		}
		if n > l.limit {
			ttl, _ := l.rdb.PTTL(c, key).Result()
			retryAfter := int(ttl.Seconds()) + 1
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(pkg.CodeRateLimited, gin.H{
				"kind": "rate limit", "msg": fmt.Sprintf("请求过于频繁，请 %d 秒后再试", retryAfter),
			})
			return
		}
		c.Next()
	}
}
