package route

import (
	"dcpro/api/middleware"
	"dcpro/db"
	"dcpro/domain"
	"dcpro/internal/embedutil"
	"dcpro/llm"
	"dcpro/repository"

	"github.com/gin-gonic/gin"
)


func HttpServering(env *domain.Env, db *db.AllDatabases,llmInvoker *llm.LLMInvoking, embedding *embedutil.Embedding, srv *gin.Engine) {
	privateRouter := srv.Group("/api")
	privateRouter.Use(middleware.JwtAuthMiddleware(env.AccessTokenSecret))

	publicRouter := srv.Group("/api")
	
	srv.GET("/hello", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, World!",
		})
	})

	repo_user := repository.NewUserRepository(env.RedisExpireTime, db.SQLDB, db.RedisDB, db.WQ)
	repo_item := repository.NewItemRepository(env.LimitTopK, env.RecsWindow, db.SQLDB, db.Milvus, db.RedisDB, db.WQ, embedding, llmInvoker)
	repo_ia := repository.NewInteractionRepository(db.RedisDB, db.WQ, env.RedisExpireTime, env.RedisIAExpireTime, env.RedisMaxLen)

	userRoute := NewUserRoute(env, privateRouter, publicRouter, repo_user)
	itemRoute := NewItemRoute(privateRouter, publicRouter, repo_item, repo_user)
	iaRoute := NewInteractionRoute(privateRouter, repo_ia)

	userRoute.RegisterRoutes()
	itemRoute.RegisterRoutes()
	iaRoute.RegisterRoutes()
}