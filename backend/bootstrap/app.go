package bootstrap

import (
	"context"
	"litstream/api/route"
	"litstream/db"
	"litstream/domain"
	"litstream/internal/queue"
	"litstream/internal/repository"
	"litstream/internal/service"
	"litstream/internal/util/embedutil"
	"litstream/internal/util/mailutil"
	"litstream/internal/worker"
	"litstream/llm"
	"litstream/pkg"
	"litstream/pkg/mylog"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// App 是进程内依赖装配的根：HTTP 与 worker 共享同一组 service（同一份业务逻辑）。
type App struct {
	Env    *domain.Env
	DBs    *db.AllDatabases
	Server *gin.Engine

	users        *service.UserService
	items        *service.ItemService
	interactions *service.InteractionService
}

func NewApp(env *domain.Env) *App {
	mylog.InitLogger(env.LogLevel)

	pkg.InitUserValidator()
	println("validator注册成功")

	dbs := &db.AllDatabases{}
	dbs.Init(env, mylog.GlobalLogger)
	println("db注册成功")

	app := &App{
		Env:    env,
		DBs:    dbs,
		Server: gin.Default(),
	}

	llmInvoker := llm.NewLLMInvoking(env.ModelApiKey, env.ModelName)
	embedder := embedutil.NewEmbedding(
		env.EmbeddingModelApiKey, env.EmbeddingModelBaseURL,
		env.VecDim, dbs.RedisDB, time.Duration(env.UserInfoTTL)*time.Hour,
		env.EmbeddingModelName)

	// 依赖装配（方向：db → repository → service → controller / worker）
	dispatcher := queue.NewDispatcher(dbs.WQ,
		time.Duration(env.QueueUniqueTTL1)*time.Second,
		time.Duration(env.QueueUniqueTTL2)*time.Second)

	userRepo := repository.NewUserRepository(
		time.Duration(env.UserNameTTL)*time.Hour,
		time.Duration(env.UserInfoTTL)*time.Hour,
		dbs.SQLDB, dbs.RedisDB)
	iaRepo := repository.NewInteractionRepository(
		dbs.RedisDB, time.Duration(env.InteractionTTL)*time.Hour,
		env.InteractionRecordMaxLen)
	itemRepo := repository.NewItemRepository(
		env.RecsWindow, dbs.SQLDB, dbs.Milvus, dbs.RedisDB,
		time.Duration(env.ItemTTL)*time.Hour)
	// 推荐结果缓存：异步精排结果保留 30 分钟，过期视为"未命中"（走粗排降级）
	recommendCache := repository.NewRecommendCache(
		dbs.RedisDB, time.Duration(env.RecommendTTL)*time.Minute)

	mailCache := repository.NewMailCache(
		dbs.RedisDB, 5*time.Minute)

	mailSender, err := mailutil.InitMail(
		env.AliyunAccessKeyId, env.AliyunAccessKeySecret, env.MailAccountName)
	if err != nil {
		log.Fatalf("init mail sender fail: %v", err)
	}

	app.users = service.NewUserService(
		userRepo, iaRepo,
		mailCache, llmInvoker, dispatcher,
		embedder, mailSender)
	app.interactions = service.NewInteractionService(iaRepo)
	app.items = service.NewItemService(
		userRepo, itemRepo, recommendCache,
		embedder, llmInvoker, dispatcher,
		env.LimitTopK, env.SearchTopK)

	route.HttpServering(
		env, app.Server, mylog.GlobalLogger, app.users, app.items, app.interactions, dbs.RedisDB)
	println("app注册成功")
	return app
}

func (app *App) CloseAllDBConnection() error {
	return app.DBs.Close()
}

func (app *App) StartServer() error {
	go func() {
		if err := app.StartWorker(); err != nil {
			log.Printf("worker can't be started: %v", err)
		}
	}()
	return app.Server.Run(app.Env.ServerAddress)
}

// StartWorker 启动 asynq worker（当前与 HTTP 同进程，逻辑上已与 HTTP 解耦）。
// 若未来需要独立扩容，可平移到独立的 cmd/worker 入口复用本方法。
func (app *App) StartWorker() error {
	redisOpt := asynq.RedisClientOpt{
		Addr:     app.Env.RedisAddress,
		Password: app.Env.RedisPassword,
		DB:       app.Env.RedisDB,
	}

	mqSrv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 50, // 控制并发，防止大量慢 SQL/LLM 打爆下游
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			requestID := mylog.RequestIDFromCtx(ctx)
			mylog.GlobalLogger.Error().
				Str("request_id", requestID).
				Str("task_type", task.Type()).
				Err(err).
				Msg("task execute failed")
		}),
	})

	mux := asynq.NewServeMux()
	mux.Use(mylog.TraceMiddleware)
	worker.RegisterUserTasks(mux, worker.NewUserWorker(app.users))
	worker.RegisterItemTasks(mux, worker.NewItemWorker(app.items))
	return mqSrv.Run(mux)
}
