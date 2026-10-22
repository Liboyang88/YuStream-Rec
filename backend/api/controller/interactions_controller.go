package controller

import (
	"litstream/domain"
	"litstream/internal/service"
	"litstream/pkg"

	"github.com/gin-gonic/gin"
)

type InteractionsController struct {
	interactions *service.InteractionService
}

func NewIaController(interactions *service.InteractionService) *InteractionsController {
	return &InteractionsController{interactions: interactions}
}

// AddNewInteraction 记录互动：同步写入后返回成功，入队/写失败不再被吞掉。
func (iac *InteractionsController) AddNewInteraction(c *gin.Context) {
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"kind": "http request", "msg": "Not authorized"})
		return
	}

	var interaction domain.Interaction
	if err := c.ShouldBindJSON(&interaction); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": "Invalid request body"})
		return
	}

	result := iac.interactions.RecordInteraction(c, uid, interaction)

	c.JSON(result.Code, result.ToGin())
}
