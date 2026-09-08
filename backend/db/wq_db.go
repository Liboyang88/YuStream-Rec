package db

import (
	"dcpro/domain"

	"github.com/hibiken/asynq"
)


func InitAsyncq(env *domain.Env) *asynq.Client {
	redisOpt := asynq.RedisClientOpt{
		Addr: env.RedisAddress,
		 Password: env.RedisPassword, DB: env.RedisDB,
	}

	client := asynq.NewClient(redisOpt)

	// 测试Redis连通性
	if err := client.Ping(); err != nil {
		// 连接失败，关闭无效client释放资源
		_ = client.Close()
		panic(err)
	}

	return client
}