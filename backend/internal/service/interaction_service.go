package service

import (
	"context"

	"litstream/domain"
	"litstream/internal/repository"
	"litstream/pkg"
)

// InteractionService 互动域业务编排。
type InteractionService struct {
	iaRepo *repository.InteractionRepository
}

// NewInteractionService 构造互动服务。
func NewInteractionService(iaRepo *repository.InteractionRepository) *InteractionService {
	return &InteractionService{iaRepo: iaRepo}
}

// RecordInteraction 记录一次互动：同步写入用户互动 stream（XAdd 毫秒级，无异步必要）。
// 前端需要立刻确认"已收到"。
func (s *InteractionService) RecordInteraction(ctx context.Context, userID uint, ia domain.Interaction) pkg.MyError {
	err := s.iaRepo.Append(ctx, userID, ia)
	if err != nil {
		return pkg.MyError{
			Code: pkg.CodeInternalServerError,
			Kind: "Record Interaction",
			Msg:  "RecordInteraction failed",
		}
	}
	return pkg.MyError{
		Code: pkg.CodeCreated,
		Kind: "Record Interaction",
		Msg:  "RecordInteraction success",
	}
}

// 预留钩子：若产品希望"每记录一次互动就自动触发画像增量更新"，
// 在此处调用 dispatcher 投递 TypeUpdateUS 任务即可（当前保持手动触发，不自动投递）。
