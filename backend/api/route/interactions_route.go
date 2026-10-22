package route

import (
	"github.com/gin-gonic/gin"

	"litstream/api/controller"
	"litstream/internal/service"
)

type InteractionsRoute struct {
	privateRouter *gin.RouterGroup
	iac           *controller.InteractionsController
}

func NewInteractionRoute(privateRouter *gin.RouterGroup, interactions *service.InteractionService) *InteractionsRoute {
	return &InteractionsRoute{privateRouter: privateRouter, iac: controller.NewIaController(interactions)}
}

func (ir *InteractionsRoute) RegisterRoutes() {
	// interactions
	ir.privateRouter.POST("/interactions", ir.iac.AddNewInteraction)
}
