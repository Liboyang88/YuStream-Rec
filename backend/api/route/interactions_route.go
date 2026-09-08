package route

import (
	"dcpro/api/controller"
	"dcpro/repository"

	"github.com/gin-gonic/gin"
)

type InteractionsRoute struct {
	privateRouter *gin.RouterGroup
	iac         *controller.InteractionsController
}
func NewInteractionRoute(privateRouter *gin.RouterGroup, interaction_repo *repository.InteractionRepository) *InteractionsRoute {
	return &InteractionsRoute{privateRouter: privateRouter, iac: controller.NewIaController(interaction_repo)}
}

func (ir *InteractionsRoute) RegisterRoutes() {
	// interactions
	ir.privateRouter.POST("/interactions", ir.iac.AddNewInteraction)
}