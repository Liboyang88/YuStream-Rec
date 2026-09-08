package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"dcpro/domain"
	"dcpro/llm"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserRepository struct {
	// env *domain.Env
	expiration time.Duration
	sqldb *gorm.DB
	redisdb *redis.Client
	qw *asynq.Client
	Anaylst *llm.LLMInvoking
}

//初始化一个User类型的仓储层
func NewUserRepository(expirationHour int, sqldb *gorm.DB, redisdb *redis.Client, qw *asynq.Client) *UserRepository {
	return &UserRepository{
		expiration: time.Duration(expirationHour) * time.Hour,
		 sqldb: sqldb, redisdb: redisdb, qw: qw}
}

/* 
查询用户名是否存在
输入：
	ctx - 上下文
	name - 用户名
输出：
	用户名是否存在+错误
错误：
	查询redis/mysql数据库时出错
*/
func (ur *UserRepository) IsNameExist(ctx context.Context, name string) (bool, error) {
	count, err := ur.redisdb.Exists(ctx, domain.KeyUserNameToID(name)).Result()
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	
	user := &domain.User{}
	err = ur.sqldb.WithContext(ctx).Where("username = ?", name).First(user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	
	return user.ID != 0, nil
}

/* 
添加新用户 向MQ发送插入mysql的任务
输入：
	ctx - 上下文
	user - 用户（用户名+密码）
输出：
	错误
错误：
	添加用户到redis/mysql数据库时出错
*/
func (ur *UserRepository) AddNewUser(ctx context.Context, user *domain.User) error {
	// 同步写 MySQL 拿到自增 ID，再写 Redis，保证 token 里 ID 正确
	if err := ur.sqldb.WithContext(ctx).Create(user).Error; err != nil {
		return err
	}

	hash, err := domain.UserToHash(user)
	if err != nil {
		return err
	}

	if err := ur.redisdb.Set(ctx, domain.KeyUserNameToID(user.Username), user.ID, ur.expiration).Err(); err != nil {
		return err
	}
	if err := ur.redisdb.HSet(ctx, domain.KeyUserIDToInfo(user.ID), hash).Err(); err != nil {
		return err
	}
	if err := ur.redisdb.Expire(ctx, domain.KeyUserIDToInfo(user.ID), ur.expiration).Err(); err != nil {
		return err
	}
	return nil
}

/* 
通过注册的请求体添加新用户（user必须为一个初始化的空结构体指针）+调用AddNewUser方法
输入：
	ctx - 上下文
	user - 用户（空）
	req - 注册请求体
输出：
	错误
错误：
	添加用户到redis/mysql数据库时出错
*/
func (ur *UserRepository) AddNewUserByReq(ctx context.Context, user *domain.User, req *domain.SignUpRequest) error {
	// 密码先 bcrypt 哈希再入库
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.Username = req.Username
	user.Password = string(hashed)

	return ur.AddNewUser(ctx, user)
}

func (ur *UserRepository) UpdateUserInfo(ctx context.Context, id uint, updateFields map[string]interface{}) error{
	// redis HSET 不接受 []string，interesting_fields 先序列化为 JSON 字符串
	redisFields := make(map[string]interface{}, len(updateFields))
	for k, v := range updateFields {
		if k == "interesting_fields" {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			redisFields[k] = string(b)
		} else {
			redisFields[k] = v
		}
	}

	if err := ur.redisdb.HSet(ctx, domain.KeyUserIDToInfo(id), redisFields).Err(); err != nil{
		return err
	}
	if err := ur.redisdb.Expire(ctx, domain.KeyUserIDToInfo(id), ur.expiration).Err(); err != nil{
		return err
	}

	task,err := domain.NewUpdateUserInfoTask(id, redisFields)
	if err != nil{
		return err
	}
	if _, err := ur.qw.Enqueue(task); err != nil{
		return err
	}
	return nil
}

func (ur *UserRepository) UpdateUserSummary(ctx context.Context, id uint) error{
	task,err := domain.NewUpdateUSTask(id)
	if err != nil{
		return err
	}
	if _, err := ur.qw.Enqueue(task); err != nil{
		return err
	}
	
	return nil
}

func (ur *UserRepository) GenerateUserSummary(ctx context.Context, id uint) error{
	task,err := domain.NewGenerateUSTask(id)
	if err != nil{
		return err
	}
	if _, err := ur.qw.Enqueue(task); err != nil{
		return err
	}
	
	return nil
}

// func (ur *UserRepository) RedisFindUserByID(ctx context.Context, id uint) (map[string]string, error){
// 	if mapStrStr, err := ur.redisdb.HGetAll(ctx, domain.KeyUserIDToProfile(id)).Result(); err != nil{
// 		return nil, err
// 	}else{
// 		if mapStrStr["id"] == ""{
// 			return nil, nil
// 		}
// 		return mapStrStr, nil
// 	}
// }

func (ur *UserRepository) FindUserByID(ctx context.Context, user *domain.User, id uint) error{

	mapStrStr, err := ur.redisdb.HGetAll(ctx, domain.KeyUserIDToInfo(id)).Result()
	if err != nil{
		return err
	}
	// 缓存命中才解析；未命中（空 map）直接回退 MySQL
	if len(mapStrStr) > 0 {
		if err := domain.HashToUser(mapStrStr, user); err == nil && user.ID != 0{
			return nil
		}
	}

	if err := ur.sqldb.WithContext(ctx).First(user, id).Error;err!= nil{
		return err
	}

	hash, err := domain.UserToHash(user)
	if err == nil {
		if err := ur.redisdb.HSet(ctx, domain.KeyUserIDToInfo(id), hash).Err(); err == nil {
			ur.redisdb.Expire(ctx, domain.KeyUserIDToInfo(id), ur.expiration)
		}
	}
	return nil
}

func (ur *UserRepository) RedisGetUIDByName(ctx context.Context, name string) (uint, error){
	if id, err := ur.redisdb.Get(ctx, domain.KeyUserNameToID(name)).Result(); err != nil{
		return 0, err
	}else{
		id1, err := strconv.ParseUint(id, 10, 32)
		if err != nil{
			return 0, err
		}
		return uint(id1), nil
	}
}

func (ur *UserRepository) RedisGetUserSummary(ctx context.Context, id uint) (string, error){
	if summary, err := ur.redisdb.HGet(ctx, domain.KeyUserIDToInfo(id), "summary").Result(); err != nil{
		return "", err
	}else{
		return summary, nil
	}
}

func (ur *UserRepository) FindUserByName(ctx context.Context, user *domain.User, name string) error{
	id, err := ur.RedisGetUIDByName(ctx, name)
	if err != nil && err != redis.Nil{
		return err
	}
	if id != 0 && err != redis.Nil{
		return ur.FindUserByID(ctx, user, id)
	}

	return ur.sqldb.WithContext(ctx).Where("username = ?", name).First(user).Error
}

func (ur *UserRepository) ExtendUnameExpire(ctx context.Context, username string) error {
	if err := ur.redisdb.Expire(ctx, domain.KeyUserNameToID(username), ur.expiration).Err(); err != nil{
		return err
	}
	return nil
}

func (ur *UserRepository) GetUserSummary(ctx context.Context, id uint) (string, error){
	summary, err := ur.RedisGetUserSummary(ctx, id)
	if err != nil{
		return "", err
	}
	if summary != ""{
		return summary, nil
	}

	user := &domain.User{}

	if err := ur.sqldb.WithContext(ctx).First(user, id).Error;err!= nil{
		return "", err
	}
	return user.Summary, nil
}