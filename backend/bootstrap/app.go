package bootstrap

import (
	"context"
	"dcpro/api/route"
	"dcpro/db"
	"dcpro/domain"
	"dcpro/internal/embedutil"
	"dcpro/llm"
	"dcpro/pkg"
	"dcpro/worker"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

type App struct {
	Env *domain.Env
	DBs  *db.AllDatabases
	Server *gin.Engine
	llmInvoker *llm.LLMInvoking
	embedding *embedutil.Embedding
}

func NewApp(env *domain.Env) *App {
	pkg.InitUserValidator()
	println("validator注册成功")
	dbs := &db.AllDatabases{}
	dbs.Init(env)
	app := &App{
		Env: env,
		DBs:  dbs,
		Server: gin.Default(),
	}
	println("app注册...")

	app.llmInvoker = llm.NewLLMInvoking(env.ModelApiKey, env.ModelName)
	app.embedding = embedutil.NewEmbedding(env)

	route.HttpServering(env, dbs, app.llmInvoker, app.embedding, app.Server)
	println("app注册成功")
	return app
}

func (app *App) CloseAllDBConnection() error {
	return app.DBs.Close()
}

func (app *App) StartServer() error {
	go func() {
		err := app.StartWorker(app.llmInvoker, app.embedding)
		if err != nil {
			println("Worker can't be started: ", err)
		}
	}()
	err := app.Server.Run(app.Env.ServerAddress)
	if err != nil {
		return err
	}
	return err
}

func (app *App) StartWorker(llmInvoker *llm.LLMInvoking, embedding *embedutil.Embedding) error {
	redisOpt := asynq.RedisClientOpt{Addr: app.Env.RedisAddress,
		 Password: app.Env.RedisPassword, DB: app.Env.RedisDB}
	
	myWorker := worker.NewMyWorker(
		app.DBs.SQLDB, app.DBs.RedisDB, 
		llmInvoker, embedding, app.Env.RedisExpireTime, 
		app.Env.RedisIAExpireTime, app.Env.RedisMaxLen)

	mq_srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 50, // 控制并发！防止同时大量SQL打崩数据库（重点）
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Printf("task failed, type:%s, err:%v", task.Type(), err)
		}),
		// Queues: map[string]int{"default": 1}, // 队列权重
	})
	mux := asynq.NewServeMux()
	// 注册任务处理器
	mux.HandleFunc(domain.TypeAddNewUser, myWorker.HandleAddNewUser)
	mux.HandleFunc(domain.TypeUpdateUserInfo, myWorker.HandleUpdateUserInfo)
	mux.HandleFunc(domain.TypeUpdateUS, myWorker.HandleUpdateUS)
	mux.HandleFunc(domain.TypeGenerateUS, myWorker.HandleGenerateUS)
	mux.HandleFunc(domain.TypeAddNewInteraction, myWorker.AddNewInteraction)
	err := mq_srv.Run(mux)
	return err
}