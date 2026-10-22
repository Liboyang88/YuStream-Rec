package db

import (
	"fmt"
	"litstream/domain"
	"litstream/pkg/mylog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// type MySQLDB struct {
// 	DB *gorm.DB
// 	env *domain.Env
// }

func InitMySQLDB(env *domain.Env, models ...any) *gorm.DB {
	var err error

	gormLogger := mylog.NewGormLogger(mylog.CtxGLKey{}, 200*time.Second, false, logger.Info)

	db, err := gorm.Open(
		mysql.Open(env.MysqlDsn),
		&gorm.Config{
			TranslateError: true,
			PrepareStmt:    true,
			Logger:         gormLogger,
			// // 2. 关闭默认事务：GORM默认每条Create/Update/Delete自动开事务；业务自己控制事务，提升性能
			// SkipDefaultTransaction: true,
		})
	if err != nil {
		panic(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(fmt.Errorf("get sqlDB failed: %w", err))
	}
	// 连接池参数，建议放到 env 环境变量，不要硬编码！
	sqlDB.SetMaxIdleConns(env.MysqlMaxIdleConns)
	sqlDB.SetMaxOpenConns(env.MysqlMaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(env.MysqlConnMaxLifetime) * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Duration(env.MysqlConnMaxIdleTime) * time.Minute)

	// 4. 连通性校验
	if err := sqlDB.Ping(); err != nil {
		panic(fmt.Errorf("mysql ping failed: %w", err))
	}

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			panic(fmt.Errorf("auto migrate failed: %w", err))
		}
	}
	return db
}

func CloseMySQLDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqldb, err := db.DB()
	if err != nil {
		return err
	}
	return sqldb.Close()
}

// Insert 插入（必须传指针，自动回填ID）
// func (m *MySQLDB) Insert(data any) error {
// 	return m.DB.Create(data).Error
// }

// // FindByID 根据ID查单条
// func (m *MySQLDB) FindByID(out any, id any) error {
// 	return m.DB.First(out, id).Error
// }

// // FindList 查询列表
// func (m *MySQLDB) Find(out any, conds ...any) error {
// 	return m.DB.Find(out, conds...).Error
// }

// // Update 更新（必须传模型指针）
// func (m *MySQLDB) Update(model any, updates any) error {
// 	return m.DB.Model(model).Updates(updates).Error
// }

// // Delete 删除（必须带主键，否则GORM拒绝删除）
// func (m *MySQLDB) Delete(model any) error {
// 	return m.DB.Delete(model).Error
// }
