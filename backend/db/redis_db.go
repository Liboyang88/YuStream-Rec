package db

import (
	"context"
	"litstream/domain"

	"github.com/redis/go-redis/v9"
)

// type RedisDB struct {
// 	DB *redis.Client
// }

func InitRedisDB(env *domain.Env) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     env.RedisAddress,
		Password: env.RedisPassword,
		DB:       env.RedisDB,
	})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		panic(err)
	}
	return rdb
}
