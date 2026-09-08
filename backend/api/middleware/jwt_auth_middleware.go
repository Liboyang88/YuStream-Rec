package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"dcpro/domain"
	"dcpro/internal/tokenutil"
)

func JwtAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Split(c.Request.Header.Get("Authorization"), " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, domain.ErrorResponse("Not authorized"))
			return
		}
		userID, err := tokenutil.ExtractIDFromToken(parts[1], secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, domain.ErrorResponse("Invalid token"))
			return
		}
		c.Set("x-user-id", userID)
		c.Next()
	}
}
