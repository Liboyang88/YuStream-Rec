package db

import (
	"dcpro/domain"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// type MySQLDB struct {
// 	DB *gorm.DB
// 	env *domain.Env
// }

func InitMySQLDB(env *domain.Env, models ...any) *gorm.DB {
	var err error

	db, err := gorm.Open(
		mysql.Open(env.MysqlDsn),
	 	&gorm.Config{})
	if err != nil {
		panic(err)
	}

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			panic(err)
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