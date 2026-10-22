package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"litstream/internal/service"
	"litstream/pkg"
)

type ItemsController struct {
	items *service.ItemService
}

func NewItemsController(items *service.ItemService) *ItemsController {
	return &ItemsController{items: items}
}

// GetItemsRecommend 异步推荐入口（同步部分只做：读缓存/粗排 + 投精排任务，不调 LLM）。
// 请求参数：?key=<上次返回的 key>（首次可省略）。
// 响应：{ "items": [...], "source": "llm"|"coarse"|"empty", "key": "<下次请求回传的 key>" }
func (itc *ItemsController) GetItemsRecommend(c *gin.Context) {
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"kind": "Get Items Recommend", "msg": "User ID not found"})
		return
	}

	res, myErr := itc.items.GetRecommend(c, uid)
	if myErr.Code >= 400 {
		c.JSON(myErr.Code, myErr.ToGin())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":   res.Items,
		"source":  res.Source,
		"outline": res.Outline,
	})
}
