package route

import(
	"github.com/gin-gonic/gin"
	"dcpro/api/controller"
	"dcpro/repository"
)

type ItemRoute struct {
	privateGroup *gin.RouterGroup
	publicGroup *gin.RouterGroup
	// env *domain.Env
	itc *controller.ItemsController
}

//NewItemRoute用于创建物品路由
func NewItemRoute(privateGroup *gin.RouterGroup, publicGroup *gin.RouterGroup,
	repo_item *repository.ItemRepository, repo_user *repository.UserRepository) *ItemRoute {
	itemRoute := &ItemRoute{privateGroup: privateGroup,publicGroup: publicGroup}

	itemRoute.itc = controller.NewItemsController(repo_item, repo_user)
	return itemRoute
}

func (it *ItemRoute) RegisterRoutes(){

	private_itg := it.privateGroup.Group("/items")
	private_itg.GET("/recommend", it.itc.GetItemsRecommend)
}