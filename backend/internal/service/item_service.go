package service

import (
	"context"
	"errors"

	"litstream/domain"
	"litstream/internal/queue"
	"litstream/internal/repository"
	"litstream/internal/util/embedutil"
	"litstream/llm"
	"litstream/pkg"
	"litstream/pkg/mylog"

	"gorm.io/gorm"
)

// 推荐响应的来源标记。
const (
	// SourceLlm 命中上一轮后台精排缓存的 LLM 结果
	SourceLlm = "llm"
	// SourceCoarse 未命中时本轮同步粗排（无 LLM）的降级结果
	SourceCoarse = "coarse"
	// SourceEmpty 空态：画像未就绪或候选为空（不是错误，前端应渲染空列表 + 提示）
	SourceEmpty = "empty"
)

const (
	StatusReady          = "ready"
	StatusProfilePending = "pending"
)

// RecommendResult 是 GET /items/recommend 的返回结果。
// Key 必须回传给前端：下次请求带 ?key=<Key>，服务端直接命中该 key 下的精排结果。
type RecommendResult struct {
	Items  []domain.Item
	Source string // "llm"（命中的精排缓存） | "coarse"（本轮粗排降级）
	// Status string // "pending"（画像生成中） | "ready"（画像已生成）
	Outline string
}

// ItemService 推荐域业务编排。
type ItemService struct {
	userRepo       *repository.UserRepository
	itemRepo       *repository.ItemRepository
	recommendCache *repository.RecommendCache
	embedder       *embedutil.Embedding
	llm            *llm.LLMInvoking
	dispatcher     *queue.Dispatcher
	limitTopK      int
	searchTopK     int
}

// NewItemService 构造推荐服务。
// limitTopK 对应 env.LimitTopK（最终推荐条数上限）。
func NewItemService(
	userRepo *repository.UserRepository,
	itemRepo *repository.ItemRepository,
	recommendCache *repository.RecommendCache,
	embedder *embedutil.Embedding,
	llmRecommender *llm.LLMInvoking,
	dispatcher *queue.Dispatcher,
	limitTopK int,
	searchTopK int,
) *ItemService {
	return &ItemService{
		userRepo:       userRepo,
		itemRepo:       itemRepo,
		recommendCache: recommendCache,
		embedder:       embedder,
		llm:            llmRecommender,
		dispatcher:     dispatcher,
		limitTopK:      limitTopK,
		searchTopK:     searchTopK,
	}
}

// GetRecommend HTTP 入口（同步，永不调 LLM）：
//  1. 用前端回传的 prevKey 查推荐结果缓存：
//     - 命中 → 直接返回上一轮 LLM 精排结果（source=llm）
//     - 未命中（无 key / key 不存在 / 过期）→ 本轮同步粗排降级返回（source=coarse）
//  2. 无论命中与否，都投 1 个精排任务到"新 key"，并把这个新 key 返回给前端；
//     下次请求带这个 key 即可命中精排结果（"永远比展示快一轮"）。
func (s *ItemService) GetRecommend(ctx context.Context, userID uint) (*RecommendResult, pkg.MyError) {
	logger := mylog.LoggerOf(ctx, mylog.CtxGLKey{})

	// 1) 命中判断
	cached, outline, err := s.recommendCache.Get(ctx, userID)
	// 如果redis get错误，进入本轮粗排降级流程 仅记录日志不返回错误
	if err != nil {
		logger.Error().Msgf("Get pre-recommend from redis cache failed: %v", err)
	}
	if cached != nil {
		logger.Info().Msgf("Get pre-recommend from redis cache success")
		// 命中：展示精排缓存
		//发送下一轮精排任务 → 下一轮展示精排结果
		if err := s.dispatcher.EnqueueRecommendation(userID, mylog.RequestIDFromCtx(ctx)); err != nil {
			logger.Error().Msgf("Enqueue recommend task failed: %v", err)
		} else {
			logger.Info().Msgf("Enqueue recommend task success")
		}
		isCount, err := s.recommendCache.Count(ctx, userID)
		if err != nil {
			logger.Error().Err(err).Uint("uid", userID).Msg("Count recommend cache failed")
		}
		if isCount {
			if err := s.dispatcher.EnqueueIncrementalProfileUpdate(userID, mylog.RequestIDFromCtx(ctx)); err != nil {
				logger.Error().Err(err).Uint("uid", userID).Msg("Enqueue incremental profile update failed")
			}
		}

		return &RecommendResult{Items: cached, Source: SourceLlm, Outline: outline}, pkg.MyError{
			Code: pkg.CodeGetSuccess,
			Kind: "Recommend Cache",
			Msg:  "命中缓存，展示精排结果",
		}
	}

	// 2) 未命中：本轮同步粗排降级（有东西可看，不阻塞等 LLM）
	// 粗排错误则返回错误
	coarse, err := s.buildCoarse(ctx, userID)
	if err != nil {
		// 画像未就绪 / 用户不存在：空态返回，绝不 5xx，并顺手补投一次画像生成任务
		if errors.Is(err, pkg.ErrSummaryNotFound) || errors.Is(err, pkg.ErrTopicsNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Info().Uint("uid", userID).Err(err).Msg("profile not ready, return empty recommend")
			if enqErr := s.dispatcher.EnqueueProfileRegeneration(userID, mylog.RequestIDFromCtx(ctx)); enqErr != nil {
				logger.Warn().Err(enqErr).Uint("uid", userID).Msg("enqueue profile regeneration failed")
				return &RecommendResult{Items: []domain.Item{}, Source: SourceEmpty}, pkg.MyError{
					Code: pkg.CodeInternalServerError,
					Kind: "Recommend Empty",
					Msg:  "用户画像尚未生成，提交生成任务失败",
				}
			}
			return &RecommendResult{Items: []domain.Item{}, Source: SourceEmpty}, pkg.MyError{
				Code: pkg.CodeGetSuccess,
				Kind: "Recommend Empty",
				Msg:  "用户画像尚未生成，已提交生成任务",
			}
		}
		logger.Error().Err(err).Uint("uid", userID).Msg("coarse recommend failed")
		return nil, pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Recommend Coarse",
			Msg:  "粗排推荐失败",
		}
	}
	//发送下一轮精排任务 → 下一轮展示精排结果
	if err := s.dispatcher.EnqueueRecommendation(userID, mylog.RequestIDFromCtx(ctx)); err != nil {
		logger.Error().Msgf("Enqueue recommend failed: %v", err)
	} else {
		logger.Info().Msgf("Enqueue recommend task success")
	}

	// 3） 粗排为空 ：返回空列表 + 提示
	if len(coarse) == 0 {
		return &RecommendResult{Items: []domain.Item{}, Source: SourceCoarse}, pkg.MyError{
			Code: pkg.CodeNotFound,
			Kind: "Recommend Coarse",
			Msg:  "暂无候选内容",
		}
	}
	return &RecommendResult{Items: coarse, Source: SourceCoarse}, pkg.MyError{
		Code: pkg.CodeGetSuccess,
		Kind: "Recommend Coarse",
		Msg:  "粗排推荐成功",
	}
}

// buildCoarse 粗排（HTTP 降级用）：只做检索候选，不调 LLM、不更新推荐窗口。
func (s *ItemService) buildCoarse(ctx context.Context, userID uint) ([]domain.Item, error) {
	_, _, items, err := s.fetchCandidates(ctx, userID, s.limitTopK)
	return items, err
}

// Recommend 精排流水线（worker 异步执行的核心实现）：
// fetchCandidates 出候选 → LLM 精排 → 记录到"最近推荐"zset。
func (s *ItemService) recommend(ctx context.Context, userID uint) ([]domain.Item, string, error) {
	summary, topics, items, err := s.fetchCandidates(ctx, userID, s.searchTopK)
	// TODO: 如果Profile空缺，则停止重试并发送生成用户画像请求
	if err != nil {
		return nil, "", err
	}
	if len(items) == 0 {
		return nil, "", nil
	}

	// LLM 精排（按候选序号让模型挑选 limitTopK 个）
	outline, err := s.llm.Recommend(ctx, userID, summary, topics, &items, len(items), s.limitTopK)
	if err != nil {
		return nil, "", err
	}

	if err := s.itemRepo.RecordRecs(ctx, userID, items); err != nil {
		mylog.LoggerOf(ctx, mylog.CtxGLKey{}).Error().Msg("Record recommend items failed")
	}
	return items, outline, nil
}

// GenerateRecommendation TypeGenerateRecommendation 任务的执行体（worker 调用）：
// 精排得到结果后写入 resultKey（供下次请求命中）；无候选则不写（保持"无 key=未命中"语义）。
func (s *ItemService) GenerateRecommendation(ctx context.Context, userID uint) error {
	items, outline, err := s.recommend(ctx, userID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	return s.recommendCache.Set(ctx, userID, items, outline)
}

// fetchCandidates 粗排候选流水线（HTTP 降级与 worker 精排共用）：
//  1. 取用户画像摘要（Redis→MySQL 回退，仓储层封装）
//  2. 摘要→向量（embedder）
//  3. Milvus 检索候选并排除最近推荐过的
//  4. 候选不足时放宽过滤条件补检
//  5. 加载候选明细（缓存+MySQL 回填）
//
// 返回摘要（供 LLM 精排用）与候选明细。
func (s *ItemService) fetchCandidates(ctx context.Context, userID uint, searchTopK int) (string, string, []domain.Item, error) {
	summary, topics, err := s.userRepo.GetSummaryAndTopics(ctx, userID)
	if err != nil {
		return "", "", nil, err
	}
	vec, err := s.embedder.GetVec(ctx, topics, userID)
	if err != nil {
		return "", "", nil, err
	}

	excludeIDs, err := s.itemRepo.RecentExcludes(ctx, userID)
	if err != nil {
		return "", "", nil, err
	}

	idList, err := s.itemRepo.SearchVec(ctx, vec, searchTopK, excludeIDs)
	if err != nil {
		return "", "", nil, err
	}
	if len(idList) > searchTopK {
		idList = idList[:searchTopK]
	}
	// 候选不足 topK：去掉过滤条件再补检一轮
	if len(idList) < searchTopK {
		more, err := s.itemRepo.SearchVec(ctx, vec, searchTopK, nil)
		if err != nil {
			return "", "", nil, err
		}
		seen := make(map[uint]struct{}, len(idList))
		for _, id := range idList {
			seen[id] = struct{}{}
		}
		for _, id := range more {
			if _, ok := seen[id]; ok {
				continue
			}
			idList = append(idList, id)
			seen[id] = struct{}{}
			if len(idList) >= searchTopK {
				break
			}
		}
	}
	if len(idList) == 0 {
		return summary, topics, nil, nil
	}

	items, err := s.itemRepo.LoadMany(ctx, idList)
	if err != nil {
		return summary, topics, nil, err
	}
	return summary, topics, items, nil
}
