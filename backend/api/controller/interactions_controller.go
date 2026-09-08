package controller

import (
	"dcpro/domain"
	"dcpro/repository"
	"net/http"
	"github.com/gin-gonic/gin"
)

type InteractionsController struct {
	// env  *domain.Env
	interaction_repo *repository.InteractionRepository
}

func NewIaController(interaction_repo *repository.InteractionRepository) *InteractionsController {
	return &InteractionsController{interaction_repo: interaction_repo, }
}

func (iac *InteractionsController) AddNewInteraction(c *gin.Context) {
	var interaction domain.Interaction
	userID, ok := c.Get("x-user-id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "x-user-id is required"})
		return
	}

	if err := c.ShouldBindJSON(&interaction); err != nil {
		c.JSON(400, gin.H{
			"error": "the request of interaction is error",
		})
		return
	}

	iac.interaction_repo.AddNewInteraction(c, interaction, userID.(uint))
	c.JSON(http.StatusOK, gin.H{"message": "add new interaction success"})
}