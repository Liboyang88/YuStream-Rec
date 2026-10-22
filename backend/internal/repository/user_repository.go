// Package repository 提供纯数据访问层：只负责 MySQL/Redis/Milvus 的读写与缓存回填，
// 不包含任何业务编排、异步任务投递或 LLM 调用——那些职责在 internal/service 层。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"litstream/domain"
	"litstream/pkg"
	"litstream/pkg/mylog"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// UserRepository 用户数据访问：MySQL 落库 + Redis 缓存读写/回填。
type UserRepository struct {
	emailTTL time.Duration // email->uid 缓存 TTL
	infoTTL  time.Duration // uid->info 缓存 TTL
	sqldb    *gorm.DB
	redisdb  *redis.Client
}

// NewUserRepository 构造用户仓储。
// expirationHour: 缓存过期小时数（对应 env.RedisExpireTime）。
func NewUserRepository(emailTTL time.Duration, infoTTL time.Duration, sqldb *gorm.DB, redisdb *redis.Client) *UserRepository {
	return &UserRepository{
		emailTTL: emailTTL,
		infoTTL:  infoTTL,
		sqldb:    sqldb,
		redisdb:  redisdb,
	}
}

// EmailExists 邮箱是否已存在：先查 Redis 缓存，未命中再回退 MySQL。
func (r *UserRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	count, err := r.redisdb.Exists(ctx, domain.KeyEmailToID(email)).Result()
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	user := &domain.User{}
	err = r.sqldb.WithContext(ctx).Where("email = ?", email).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return user.Email != "", nil
}

// CreateUser 注册落库：MySQL insert（拿自增 ID）后初始化 Redis 缓存
// （name→id、id→info hash、TTL）。由注册业务（同步）调用。
func (r *UserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	if err := r.sqldb.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return pkg.ErrUserEmailTaken // 并发注册同邮箱：以唯一索引为准，映射 409
		}
		return err
	}

	if err := r.initUserCache(ctx, user); err != nil {
		return fmt.Errorf("%w: %v", pkg.ErrUserCacheInit, err)
	}
	return nil
}

func (r *UserRepository) initUserCache(ctx context.Context, user *domain.User) error {
	hash, err := domain.UserToHash(user)
	if err != nil {
		return err
	}
	if err := r.redisdb.Set(ctx, domain.KeyEmailToID(user.Email), user.ID, r.emailTTL).Err(); err != nil {
		return err
	}
	if err := r.redisdb.HSet(ctx, domain.KeyUserIDToInfo(user.ID), hash).Err(); err != nil {
		return err
	}
	return r.redisdb.Expire(ctx, domain.KeyUserIDToInfo(user.ID), r.infoTTL).Err()
}

// FindByID 按 ID 读用户：Redis hash 命中即返回；未命中回退 MySQL 并回填缓存。
// 用户不存在时返回 gorm.ErrRecordNotFound。
func (r *UserRepository) FindByID(ctx context.Context, id uint) (*domain.User, error) {
	hash, err := r.redisdb.HGetAll(ctx, domain.KeyUserIDToInfo(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(hash) > 0 {
		user := &domain.User{}
		if err := domain.HashToUser(hash, user); err == nil && user.ID != 0 {
			return user, nil
		}
	}

	user := &domain.User{}
	if err := r.sqldb.WithContext(ctx).First(user, id).Error; err != nil {
		return nil, err
	}
	// 回填缓存（尽力而为，失败不影响主流程）
	if backfill, err := domain.UserToHash(user); err == nil {
		if err := r.redisdb.HSet(ctx, domain.KeyUserIDToInfo(id), backfill).Err(); err == nil {
			_ = r.redisdb.Expire(ctx, domain.KeyUserIDToInfo(id), r.infoTTL).Err()
		}
	}
	return user, nil
}

func (r *UserRepository) FindForAuthByEmail(ctx context.Context, email string) (*domain.User, error) {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	var uid = uint(0)
	user := &domain.User{}
	// 先从 Redis 缓存中获取 ID
	idStr, err := r.redisdb.Get(ctx, domain.KeyEmailToID(email)).Result()
	if err == nil {
		// 缓存命中，进行类型转换
		id64, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			logger.Error().Err(err).Msg("Redis Get error")
		} else {
			uid = uint(id64)
		}
	} else if !errors.Is(err, redis.Nil) {
		// 如果缓存没有命中，且不是 nil 错误，则redis错误
		// 进行log记录，不返回错误，继续进行MySQL兜底
		logger.Error().Err(err).Msg("Redis Get error")
	}
	if uid == 0 {
		user.Email = email
		if err := r.sqldb.WithContext(ctx).Where("email = ?", email).First(user).Error; err != nil {
			return nil, err
		}
	} else {
		if err := r.sqldb.WithContext(ctx).First(user, uid).Error; err != nil {
			return nil, err
		}
	}

	if user.Password == "" {
		return nil, pkg.ErrPasswordEmpty
	}

	return user, nil
}

// UpdateMySQL 更新用户资料（只写 MySQL）。业务编排见 service.UserService.UpdateProfile。
func (r *UserRepository) UpdateMySQL(ctx context.Context, id uint, cols map[string]any) error {
	return r.sqldb.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).Updates(cols).Error
}

func (r *UserRepository) UpdatePassword(ctx context.Context, email string, password string) error {
	return r.sqldb.WithContext(ctx).Model(&domain.User{}).Where("email = ?", email).Update("password", password).Error
}

// PatchCache 把资料更新同步到 Redis 缓存 hash 并续期。
// 负责存储格式差异的转换：[]string（interesting_fields）序列化为 JSON 字符串、int 转字符串等。
func (r *UserRepository) PatchCache(ctx context.Context, id uint, cols map[string]any) error {
	if err := r.redisdb.HSet(ctx, domain.KeyUserIDToInfo(id), cols).Err(); err != nil {
		return err
	}
	return r.redisdb.Expire(ctx, domain.KeyUserIDToInfo(id), r.infoTTL).Err()
}

// GetSummary 读取用户画像摘要：Redis 命中即返回，未命中回退 MySQL。
func (r *UserRepository) GetSummaryAndTopics(ctx context.Context, id uint) (string, string, error) {
	key := domain.KeyUserIDToInfo(id)
	summary, err := r.redisdb.HGet(ctx, key, "summary").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", "", err
	}
	topics, err := r.redisdb.HGet(ctx, key, "topics").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", "", err
	}
	if summary != "" && topics != "" {
		return summary, topics, nil
	}

	user := &domain.User{}
	if err := r.sqldb.WithContext(ctx).First(user, id).Error; err != nil {
		return "", "", err
	}
	if user.Summary == "" {
		return "", "", pkg.ErrSummaryNotFound
	}
	if user.Topics == "" {
		return "", "", pkg.ErrTopicsNotFound
	}
	r.backfillProfileCache(ctx, id, user)
	return user.Summary, user.Topics, nil
}

// backfillProfileCache 回填用户画像缓存（尽力而为：失败只记日志，不影响主流程）。
func (r *UserRepository) backfillProfileCache(ctx context.Context, id uint, user *domain.User) {
	hash, err := domain.UserToHash(user)
	if err != nil {
		return
	}
	key := domain.KeyUserIDToInfo(id)
	if err := r.redisdb.HSet(ctx, key, hash).Err(); err != nil {
		return
	}
	_ = r.redisdb.Expire(ctx, key, r.infoTTL).Err()
}

// SaveSummary 写回用户画像摘要：先更新 Redis 缓存并续期，再落 MySQL。
func (r *UserRepository) SaveSummaryAndTopics(ctx context.Context, id uint, summary string, topics string) error {
	key := domain.KeyUserIDToInfo(id)
	vecKey := domain.KeyTopicsVec(id)

	// 用事务性 pipeline:HSet + HDel 一起生效,避免"新摘要 + 旧向量"的中间态暴露给读取方
	pipe := r.redisdb.TxPipeline()
	pipe.HSet(ctx, key, "summary", summary)
	pipe.HSet(ctx, key, "topics", topics)
	pipe.Del(ctx, vecKey) // ← 核心:摘要变了,向量立即失效(下次 GetVec 自动重算)
	pipe.Expire(ctx, key, r.infoTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return r.sqldb.WithContext(ctx).Model(&domain.User{}).
		Where("id = ?", id).Updates(map[string]any{"summary": summary, "topics": topics}).Error
}

// TouchNameTTL 登录成功后给 name→id 缓存续期。
// func (r *UserRepository) TouchNameTTL(ctx context.Context, username string) error {
// 	return r.redisdb.Expire(ctx, domain.KeyUserNameToID(username), r.nameTTL).Err()
// }

func (r *UserRepository) TouchEmailTTL(ctx context.Context, email string) error {
	return r.redisdb.Expire(ctx, domain.KeyEmailToID(email), r.emailTTL).Err()
}
