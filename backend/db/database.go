package db

import (
	"errors"
	"litstream/domain"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type AllDatabases struct {
	SQLDB   *gorm.DB
	RedisDB *redis.Client
	WQ      *asynq.Client
	Milvus  *MilvusDB
}

func (a *AllDatabases) Init(env *domain.Env, logger *zerolog.Logger) error {

	var err error

	a.SQLDB = InitMySQLDB(env, &domain.User{}, &domain.Item{})
	logger.Info().Msg("mysql数据库注册成功")
	a.RedisDB = InitRedisDB(env)
	logger.Info().Msg("redis数据库注册成功")
	a.WQ = InitAsynq(env)
	logger.Info().Msg("asynq数据库注册成功")
	a.Milvus, err = InitMilvus(env)
	if err != nil {
		panic(err)
	}
	logger.Info().Msg("milvus数据库注册成功")
	logger.Info().Msg("数据库注册成功")
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
