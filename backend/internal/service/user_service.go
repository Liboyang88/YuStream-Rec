// Package service 是业务编排层（use-case）：每个业务"该同步直做还是异步投递"在这里显式决定。
// service 只依赖 repository（纯存取）、llm、queue（任务投递口），
// 不感知 HTTP（gin）与任务队列实现（asynq）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"litstream/domain"
	"litstream/internal/queue"
	"litstream/internal/repository"
	"litstream/internal/util/embedutil"
	"litstream/internal/util/mailutil"
	"litstream/llm"
	"litstream/pkg"
	"litstream/pkg/mylog"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserService 用户域业务编排。
type UserService struct {
	userRepo   *repository.UserRepository
	iaRepo     *repository.InteractionRepository
	mailCache  *repository.MailCache
	llm        *llm.LLMInvoking
	dispatcher *queue.Dispatcher
	embedder   *embedutil.Embedding
	mail       *mailutil.Mail
}

// NewUserService 构造用户服务。
func NewUserService(
	userRepo *repository.UserRepository,
	iaRepo *repository.InteractionRepository,
	mailCache *repository.MailCache,
	llmInvoking *llm.LLMInvoking,
	dispatcher *queue.Dispatcher,
	embedder *embedutil.Embedding,
	mail *mailutil.Mail,
) *UserService {
	return &UserService{
		userRepo:   userRepo,
		iaRepo:     iaRepo,
		mailCache:  mailCache,
		llm:        llmInvoking,
		dispatcher: dispatcher,
		embedder:   embedder,
		mail:       mail,
	}
}

// ===== 同步业务（HTTP 请求内完成） =====

// Register 注册：校验用户名占用 → bcrypt 加密 → 落库并初始化缓存。
// 必须同步：需要立刻拿到自增 ID 签发 token。
func (s *UserService) Register(ctx context.Context, email, username, password, thisCode string) (*domain.User, pkg.MyError) {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})

	isVerify, err := s.mailCache.Verify(ctx, email, thisCode)
	if err != nil {
		logger.Error().Msgf("Register failed-Verify: %v", err)
	}
	if !isVerify {
		return nil, pkg.MyError{
			Code: pkg.CodeBadRequest,
			Kind: "Register",
			Msg:  "验证码错误",
		}
	}

	exists, err := s.userRepo.EmailExists(ctx, email)
	if err != nil {
		logger.Error().Msgf("Register failed-NameExists: %v", err)
		return nil, pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Register",
			Msg:  "后台错误导致注册失败，请稍后再试",
		}
	}
	if exists {
		return nil, pkg.MyError{
			Code: pkg.CodeConflict,
			Kind: "Register",
			Msg:  "该邮箱已关联账号",
		}
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error().Msgf("Register failed-GenerateFromPassword: %v", err)
		return nil, pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Register",
			Msg:  "后台错误导致注册失败，请稍后再试",
		}
	}

	user := &domain.User{
		Email:    email,
		Username: username,
		Password: string(hashed),
	}
	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		switch {
		case errors.Is(err, pkg.ErrUserEmailTaken):
			return nil, pkg.MyError{Code: pkg.CodeConflict, Kind: "Register", Msg: "该邮箱已关联其他账号，请找回密码或使用其他邮箱注册"}
		case errors.Is(err, pkg.ErrUserCacheInit):
			// 账号已创建成功：记日志并继续签发 token，绝不返回 500
			logger.Warn().Err(err).Uint("uid", user.ID).Msg("user created but cache init failed")
		default:
			logger.Error().Err(err).Msg("Register failed-CreateUser")
			return nil, pkg.MyError{Code: pkg.CodeInternalServerError, Kind: "Register", Msg: "Register failed"}
		}
	}
	return user, pkg.MyError{
		Code: pkg.CodeCreated,
		Kind: "Register",
		Msg:  "注册成功",
	}
}

func (s *UserService) Authenticate(ctx context.Context, email, password string) (*domain.User, pkg.MyError) {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	user, err := s.userRepo.FindForAuthByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pkg.MyError{
				Code: pkg.CodeUnauthorized,
				Kind: "Authenticate",
				Msg:  "邮箱或密码错误",
			}
		}
		if errors.Is(err, pkg.ErrPasswordEmpty) {
			return nil, pkg.MyError{
				Code: pkg.CodeInternalServerError,
				Kind: "Authenticate",
				Msg:  "后端鉴权错误",
			}
		}
		return nil, pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Authenticate",
			Msg:  "后端搜索邮箱时出现错误",
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, pkg.MyError{
			Code: pkg.CodeUnauthorized,
			Kind: "Authenticate",
			Msg:  "邮箱或密码错误",
		}
	}

	if err := s.userRepo.TouchEmailTTL(ctx, email); err != nil {
		logger.Error().Msgf("Authenticate failed-TouchEmailTTL: %v", err)
	}
	return user, pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "Authenticate",
		Msg:  "登录成功",
	}
}

func (s *UserService) ForgetPassword(ctx context.Context, email, thisCode, newPassword string) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	isVerify, err := s.mailCache.Verify(ctx, email, thisCode)
	if err != nil {
		logger.Error().Msgf("Register failed-Verify: %v", err)
	}
	if !isVerify {
		return pkg.MyError{
			Code: pkg.CodeBadRequest,
			Kind: "Forget Password",
			Msg:  "验证码错误",
		}
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Forget Password",
			Msg:  "后端错误，请稍后再试",
		}
	}

	if err := s.userRepo.UpdatePassword(ctx, email, string(hashed)); err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Forget Password",
			Msg:  "后端错误，请稍后再试",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "Forget Password",
		Msg:  "密码重置成功",
	}
}

func (s *UserService) CheckEmailExists(ctx context.Context, email string) pkg.MyError {
	exists, err := s.userRepo.EmailExists(ctx, email)
	if err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Check Email Exists",
			Msg:  "后端错误，请稍后再试",
		}
	}
	if !exists {
		return pkg.MyError{
			Code: pkg.CodeNotFound,
			Kind: "Check Email Exists",
			Msg:  "该邮箱还未注册账号",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "Check Email Exists",
		Msg:  "该邮箱已关联账号",
	}
}

func (s *UserService) SendVerifyMail(ctx context.Context, email string, isSignUp bool) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	seconds, err := s.mailCache.Ttl(ctx, email)
	// ttl>4min 则说明没超出冷却期
	if seconds > 4*60 {
		return pkg.MyError{
			Code: pkg.CodeConflict,
			Kind: "Send Email",
			Msg:  fmt.Sprintf("已发送验证码，请%ds后重试", seconds-4*60),
		}
	}
	if err != nil {
		logger.Error().Msg(err.Error())
	}

	verifyCode, err := s.mail.Send(email, isSignUp)
	if err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Send Email",
			Msg:  "发送验证码失败，检查邮箱地址是否有效或稍后再试",
		}
	}

	if err := s.mailCache.Set(ctx, email, verifyCode); err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Send Email",
			Msg:  "发送验证码失败，检查邮箱地址是否有效或稍后再试",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeAccepted,
		Kind: "Send Email",
		Msg:  "验证码已发送，请注意查收",
	}
}

// GetProfile 读取用户资料（缓存/DB 回退细节在仓储层）。
func (s *UserService) GetProfile(ctx context.Context, userID uint) (*domain.User, pkg.MyError) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pkg.MyError{
				Code: pkg.CodeNotFound,
				Kind: "GetProfile",
				Msg:  "该用户不存在",
			}
		}
		return nil, pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "GetProfile",
			Msg:  "搜索用户时出现错误",
		}
	}
	return user, pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "GetProfile",
		Msg:  "获取用户资料成功",
	}
}

func (s *UserService) FillProfile(ctx context.Context, userID uint, patch *domain.InfoRequest) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	cols := buildProfileCols(patch)
	if len(cols) == 0 {
		return pkg.MyError{
			Code: pkg.CodeBadRequest,
			Kind: "UpdateProfile",
			Msg:  "当前没有改动的字段，请至少修改一个字段",
		}
	}
	if err := s.userRepo.UpdateMySQL(ctx, userID, cols); err != nil {
		logger.Error().Msgf("FillProfile failed-UpdateMySQL: %v", err)
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "FillProfile",
			Msg:  "填写用户资料失败",
		}
	}
	// TODO: 更新缓存失败要不要返回请求失败？要不要回滚 MySQL 操作？
	if err := s.userRepo.PatchCache(ctx, userID, cols); err != nil {
		// MySQL 已提交，缓存失败不回滚；下次读取会回填缓存自愈。
		logger.Error().Msgf("patch user cache failed: %v", err)
	}
	// if err := s.dispatcher.EnqueueProfileRegeneration(userID); err != nil {
	// 	logger.Error().Msgf("FillProfile failed-EnqueueUpdateUSWithNewInfo: %v", err)
	// 	return pkg.MyError{
	// 		Code: pkg.CodeInternalServerError,
	// 		Kind: "FillProfile",
	// 		Msg:  "提交`生成用户画像`任务失败",
	// 	}
	// }
	return pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "FillProfile",
		Msg:  "填写用户资料成功",
	}
}

// UpdateProfile 改资料：整笔同步完成（先 MySQL 落库，后 Redis 更新缓存）。
func (s *UserService) UpdateProfile(ctx context.Context, userID uint, patch *domain.InfoRequest) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	cols := buildProfileCols(patch)
	if len(cols) == 0 {
		return pkg.MyError{
			Code: pkg.CodeBadRequest,
			Kind: "UpdateProfile",
			Msg:  "当前没有改动的字段，请至少修改一个字段",
		}
	}
	if err := s.userRepo.UpdateMySQL(ctx, userID, cols); err != nil {
		logger.Error().Msgf("UpdateProfile failed-UpdateMySQL: %v", err)
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "UpdateProfile",
			Msg:  "更新用户资料失败",
		}
	}
	// TODO: 更新缓存失败要不要返回请求失败？要不要回滚 MySQL 操作？
	if err := s.userRepo.PatchCache(ctx, userID, cols); err != nil {
		// MySQL 已提交，缓存失败不回滚；下次读取会回填缓存自愈。
		logger.Error().Msgf("patch user cache failed: %v", err)
	}
	if err := s.dispatcher.EnqueueUpdateUSWithNewInfo(userID, cols, mylog.RequestIDFromCtx(ctx)); err != nil {
		logger.Error().Msgf("UpdateProfile failed-EnqueueUpdateUSWithNewInfo: %v", err)
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "UpdateProfile",
			Msg:  "提交`更新用户画像`任务失败",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "UpdateProfile",
		Msg:  "更新用户资料成功",
	}
}

// buildProfileCols 把请求补丁转成 MySQL 列映射（nil 字段不更新）。
func buildProfileCols(req *domain.InfoRequest) map[string]any {
	cols := make(map[string]any, 7)
	if req.Age != nil {
		cols["age"] = *req.Age
	}
	if req.Gender != nil {
		cols["gender"] = *req.Gender
	}
	if req.Job != nil {
		cols["job"] = *req.Job
	}
	if req.Education != nil {
		cols["education"] = *req.Education
	}
	if req.Location != nil {
		cols["location"] = *req.Location
	}
	if req.Organization != nil {
		cols["organization"] = *req.Organization
	}
	if req.InterestingFields != nil {
		jsonStr, err := json.Marshal(*req.InterestingFields)
		if err == nil {
			cols["interesting_fields"] = string(jsonStr)
		}
	}
	return cols
}

// ===== 异步业务：入队入口（HTTP 调用，秒回"已受理"） =====

// EnqueueIncrementalProfileUpdate 触发"增量更新用户画像"：只入队，真正执行见 IncrementalProfileUpdate。
func (s *UserService) EnqueueIncrementalProfileUpdate(ctx context.Context, userID uint) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	err := s.dispatcher.EnqueueIncrementalProfileUpdate(userID, mylog.RequestIDFromCtx(ctx))
	if err != nil {
		logger.Error().Msgf("EnqueueIncrementalProfileUpdate failed: %v", err)
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "EnqueueIncrementalProfileUpdate",
			Msg:  "提交`增量更新用户画像`任务失败",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeAccepted,
		Kind: "EnqueueIncrementalProfileUpdate",
		Msg:  "提交`增量更新用户画像`任务成功",
	}
}

// EnqueueProfileRegeneration 触发"重新生成用户画像"：只入队，真正执行见 RegenerateProfile。
func (s *UserService) EnqueueProfileRegeneration(ctx context.Context, userID uint) pkg.MyError {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})
	err := s.dispatcher.EnqueueProfileRegeneration(userID, mylog.RequestIDFromCtx(ctx))
	if err != nil {
		logger.Error().Msgf("EnqueueProfileRegeneration failed: %v", err)
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "EnqueueProfileRegeneration",
			Msg:  "提交`生成用户画像`任务失败",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeAccepted,
		Kind: "EnqueueProfileRegeneration",
		Msg:  "提交`生成用户画像`任务成功",
	}
}

// ===== 异步业务：执行体（worker 拆单后调用，业务逻辑唯一实现） =====

// IncrementalProfileUpdate TypeUpdateUS 任务的执行体：
// 读互动 stream → 切 new/his → LLM 增量更新画像 → 写回缓存与 MySQL → 推进指针。
// 指针在业务成功后推进，避免 LLM 失败重试时互动被跳过。
func (s *UserService) IncrementalProfileUpdate(ctx context.Context, userID uint) error {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return err
	}

	newIA, hisIA, latestID, err := s.iaRepo.FetchDelta(ctx, userID)
	if err != nil {
		return err
	}
	if latestID == "" {
		return nil // 没有新互动，无需更新
	}

	interestTopics, err := s.llm.UpdateUserProfile(ctx, user, &newIA, &hisIA)
	if err != nil {
		return err
	}
	if err := s.userRepo.SaveSummaryAndTopics(ctx, userID, user.Summary, interestTopics); err != nil {
		return err
	}
	if err := s.iaRepo.AdvancePointer(ctx, userID, latestID); err != nil {
		return err
	}
	_, err = s.embedder.Encode(ctx, interestTopics, userID)
	if err != nil {
		log.Printf("encode summary failed, uid:%d err:%v", userID, err)
	}
	return nil
}

// func (s *UserService) firstGenerateProfile(ctx context.Context, user *domain.User) error {
// 	interestTopics, err := s.llm.GenerateUserProfile(ctx, user)
// 	if err != nil {
// 		return err
// 	}
// 	if err := s.userRepo.SaveSummaryAndTopics(ctx, user, user.Summary, interestTopics); err != nil {
// 		return err
// 	}
// 	_, err = s.embedder.Encode(ctx, interestTopics, userID)
// 	if err != nil {
// 		log.Printf("encode summary failed, uid:%d err:%v", userID, err)
// 	}
// 	return nil
// }

// GenerateProfile TypeGenerateUS 任务的执行体：
// 加载用户 → LLM 依据资料重新生成画像 → 写回缓存与 MySQL。
func (s *UserService) GenerateProfile(ctx context.Context, userID uint) error {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	interestTopics, err := s.llm.GenerateUserProfile(ctx, user)
	if err != nil {
		return err
	}
	if err := s.userRepo.SaveSummaryAndTopics(ctx, userID, user.Summary, interestTopics); err != nil {
		return err
	}
	_, err = s.embedder.Encode(ctx, interestTopics, userID)
	if err != nil {
		log.Printf("encode summary failed, uid:%d err:%v", userID, err)
	}
	return nil
}

// ReProfileWithNewInfo TypeUpdateUS 任务的执行体：
// 加载用户 → LLM 依据资料重新生成画像 → 写回缓存与 MySQL。
func (s *UserService) ReProfileWithNewInfo(ctx context.Context, userID uint, colsToStr string) error {
	// user, err := s.userRepo.FindByID(ctx, userID)
	// if err != nil {
	// 	return err
	// }
	summary, interestTopics, err := s.llm.UpdateWithNewInfo(ctx, colsToStr)
	if err != nil {
		return err
	}
	if err := s.userRepo.SaveSummaryAndTopics(ctx, userID, summary, interestTopics); err != nil {
		return err
	}
	_, err = s.embedder.Encode(ctx, interestTopics, userID)
	if err != nil {
		log.Printf("encode summary failed, uid:%d err:%v", userID, err)
	}
	return nil
}
