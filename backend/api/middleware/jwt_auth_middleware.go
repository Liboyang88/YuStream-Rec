package middleware

import (
	"litstream/internal/util/tokenutil"
	"litstream/pkg"
	"strings"

	"github.com/gin-gonic/gin"
)

func JwtAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Split(c.Request.Header.Get("Authorization"), " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(pkg.CodeUnauthorized, gin.H{"kind": "http request", "msg": "Not authorized"})
			return
		}
		userID, err := tokenutil.ExtractIDFromToken(parts[1], secret)
		if err != nil {
			c.AbortWithStatusJSON(pkg.CodeUnauthorized, gin.H{"kind": "http request", "msg": "Invalid token"})
			return
		}
		c.Set("x-user-id", userID)
		c.Next()
	}
}
