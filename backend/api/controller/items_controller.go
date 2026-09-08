package controller

import (
	"dcpro/domain"
	"dcpro/repository"
	"net/http"
	"github.com/gin-gonic/gin"
)

type ItemsController struct {
	repo_item *repository.ItemRepository
	repo_user *repository.UserRepository
}

func NewItemsController(repo_item *repository.ItemRepository, repo_user *repository.UserRepository) *ItemsController {
	return &ItemsController{repo_item: repo_item, repo_user: repo_user}
}

func (itc *ItemsController) GetItemsRecommend(c *gin.Context) {
	userID, ok := c.Get("x-user-id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "x-user-id is required"})
		return
	}

	userSummary, err := itc.repo_user.GetUserSummary(c, userID.(uint))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	items := &[]domain.Item{}

	err = itc.repo_item.GetItemsByIds(c, userID.(uint), userSummary, items)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

