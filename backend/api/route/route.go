package route

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"litstream/api/middleware"
	"litstream/domain"
	"litstream/internal/service"
)

// HttpServering 注册全部 HTTP 路由。
// controller 只依赖 service 层；repository/queue 等由 bootstrap 装配好再传入。
func HttpServering(env *domain.Env, srv *gin.Engine, logger *zerolog.Logger,
	users *service.UserService, items *service.ItemService,
	interactions *service.InteractionService, rdb *redis.Client) {

	loginLimit := middleware.NewRateLimiter(rdb, "rl:login:ip", 10, time.Minute)
	signupLimit := middleware.NewRateLimiter(rdb, "rl:signup:ip", 5, time.Minute)
	forgetPasswordLimit := middleware.NewRateLimiter(rdb, "rl:forget-password:ip", 100, 5*time.Minute)
	sendEmailLimit := middleware.NewRateLimiter(rdb, "rl:send-email:ip", 10, time.Hour*24)

	srv.Use(middleware.ZerologGinMiddleware(logger))
	privateRouter := srv.Group("/api")
	privateRouter.Use(middleware.JwtAuthMiddleware(env.AccessTokenSecret))

	publicRouter := srv.Group("/api")

	srv.GET("/hello", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, World!",
		})
	})

	userRoute := NewUserRoute(
		env, privateRouter, publicRouter, users,
		loginLimit, signupLimit, forgetPasswordLimit, sendEmailLimit)
	itemRoute := NewItemRoute(privateRouter, publicRouter, items)
	iaRoute := NewInteractionRoute(privateRouter, interactions)

	userRoute.RegisterRoutes()
	itemRoute.RegisterRoutes()
	iaRoute.RegisterRoutes()
}
