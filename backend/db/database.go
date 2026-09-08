package db

import (
	"errors"

	"dcpro/domain"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type AllDatabases struct {
	SQLDB *gorm.DB
	RedisDB *redis.Client
	WQ *asynq.Client
	Milvus *MilvusDB
}

func (a *AllDatabases) Init(env *domain.Env) error {

	var err error

	a.SQLDB = InitMySQLDB(env, &domain.User{}, &domain.Item{})
	println("mysql数据库注册成功")
	a.RedisDB = InitRedisDB(env)
	println("redis数据库注册成功")
	a.WQ = InitAsyncq(env)
	println("asyncq数据库注册成功")
	a.Milvus, err = InitMilvus(env)
	println("milvus数据库注册成功")
	if err != nil {
		panic(err)
	}
	println("数据库注册成功")
	return nil
}

func (a *AllDatabases) Close() error {
	var errs []error
	if a.SQLDB != nil {
		if sqlDB, err := a.SQLDB.DB(); err == nil {
			errs = append(errs, sqlDB.Close())
		} else {
			errs = append(errs, err)
		}
	}
	if a.RedisDB != nil {
		errs = append(errs, a.RedisDB.Close())
	}
	if a.WQ != nil {
		errs = append(errs, a.WQ.Close())
	}
	if a.Milvus != nil {
		errs = append(errs, a.Milvus.Close())
	}
	return errors.Join(errs...)
}
