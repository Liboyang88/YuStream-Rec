package route

import (
	"github.com/gin-gonic/gin"

	"litstream/api/controller"
	"litstream/api/middleware"
	"litstream/domain"
	"litstream/internal/service"
)

type UserRoute struct {
	privateGroup        *gin.RouterGroup
	publicGroup         *gin.RouterGroup
	env                 *domain.Env
	uc                  *controller.UsersController
	loginLimit          *middleware.RateLimiter
	signupLimit         *middleware.RateLimiter
	forgetPasswordLimit *middleware.RateLimiter
	sendEmailLimit      *middleware.RateLimiter
}

// NewUserRoute 用于创建用户路由。
func NewUserRoute(
	env *domain.Env, privateGroup *gin.RouterGroup, publicGroup *gin.RouterGroup,
	users *service.UserService,
	loginLimit, signupLimit, forgetPasswordLimit, sendEmailLimit *middleware.RateLimiter) *UserRoute {
	userRoute := &UserRoute{
		privateGroup:        privateGroup,
		publicGroup:         publicGroup,
		env:                 env,
		loginLimit:          loginLimit,
		signupLimit:         signupLimit,
		forgetPasswordLimit: forgetPasswordLimit,
		sendEmailLimit:      sendEmailLimit}
	userRoute.uc = controller.NewUsersController(users,
		env.AccessTokenSecret, env.AccessTokenExpire,
		env.RefreshTokenSecret, env.RefreshTokenExpire)
	return userRoute
}

func (u *UserRoute) RegisterRoutes() {
	private_ug := u.privateGroup.Group("/users")

	private_ug.POST("/summary", u.uc.UpdateSummary)
	private_ug.PUT("/summary", u.uc.GenerateSummary)

	private_ug.GET("/info", u.uc.GetProfile)
	private_ug.POST("/info", u.uc.FillInfo)
	private_ug.PUT("/info", u.uc.UpdateInfo)

	public_ug := u.publicGroup.Group("/users")

	public_ug.POST("/login",
		u.loginLimit.Limit(func(c *gin.Context) string { return c.ClientIP() }),
		u.uc.Login)
	public_ug.POST("/signup",
		u.signupLimit.Limit(func(c *gin.Context) string { return c.ClientIP() }),
		u.uc.SignUp)
	public_ug.POST("/signup/email",
		u.sendEmailLimit.Limit(func(c *gin.Context) string { return c.ClientIP() }),
		u.uc.SendEmailForSignUp)

	public_ug.PATCH("/forget-password",
		u.forgetPasswordLimit.Limit(func(c *gin.Context) string { return c.ClientIP() }),
		u.uc.ForgetPassword)
	public_ug.POST("/forget-password/email",
		u.sendEmailLimit.Limit(func(c *gin.Context) string { return c.ClientIP() }),
		u.uc.SendEmailForForgetPassword)
}
