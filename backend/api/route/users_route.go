package route

import(
	"github.com/gin-gonic/gin"
	"dcpro/api/controller"
	"dcpro/repository"
	"dcpro/domain"
)

type UserRoute struct {
	privateGroup *gin.RouterGroup
	publicGroup *gin.RouterGroup
	env *domain.Env
	uc *controller.UsersController
}

//NewUserRoute用于创建用户路由
func NewUserRoute(env *domain.Env, privateGroup *gin.RouterGroup, publicGroup *gin.RouterGroup, repo_user *repository.UserRepository) *UserRoute {
	userRoute := &UserRoute{privateGroup: privateGroup,publicGroup: publicGroup, env: env}

	userRoute.uc = controller.NewUsersController(repo_user, env.AccessTokenSecret, env.AccessTokenExpire, env.RefreshTokenSecret, env.RefreshTokenExpire)
	return userRoute
}

func (u *UserRoute) RegisterRoutes(){

	private_ug := u.privateGroup.Group("/users")

	private_ug.POST("/summary", u.uc.UpdateSummary)
	private_ug.PUT("/summary", u.uc.GenerateSummary)

	private_ug.GET("/info", u.uc.GetProfile)
	private_ug.POST("/info", u.uc.UpdateInfo)
	private_ug.PUT("/info", u.uc.UpdateInfo)

	public_ug := u.publicGroup.Group("/users")
	
	public_ug.POST("/signup", u.uc.SignUp)
	public_ug.POST("/login", u.uc.Login)
}