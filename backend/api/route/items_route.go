package route

import (
	"github.com/gin-gonic/gin"

	"litstream/api/controller"
	"litstream/internal/service"
)

type ItemRoute struct {
	privateGroup *gin.RouterGroup
	publicGroup  *gin.RouterGroup
	itc          *controller.ItemsController
}

// NewItemRoute 用于创建物品路由。
func NewItemRoute(privateGroup *gin.RouterGroup, publicGroup *gin.RouterGroup,
	items *service.ItemService) *ItemRoute {
	itemRoute := &ItemRoute{privateGroup: privateGroup, publicGroup: publicGroup}
	itemRoute.itc = controller.NewItemsController(items)
	return itemRoute
}

func (it *ItemRoute) RegisterRoutes() {
	private_itg := it.privateGroup.Group("/items")
	private_itg.GET("/recommend", it.itc.GetItemsRecommend)
}
