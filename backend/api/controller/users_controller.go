package controller

import (
	"github.com/gin-gonic/gin"

	"litstream/domain"
	"litstream/internal/service"
	"litstream/internal/util/tokenutil"
	"litstream/pkg"
)

type UsersController struct {
	users              *service.UserService
	AccessTokenSecret  string
	AccessTokenExpire  int
	RefreshTokenSecret string
	RefreshTokenExpire int
}

func NewUsersController(
	users *service.UserService,
	accessTokenSecret string, accessTokenExpire int,
	refreshTokenSecret string, refreshTokenExpire int) *UsersController {
	return &UsersController{
		users:              users,
		AccessTokenSecret:  accessTokenSecret,
		AccessTokenExpire:  accessTokenExpire,
		RefreshTokenSecret: refreshTokenSecret,
		RefreshTokenExpire: refreshTokenExpire,
	}
}

func (uc *UsersController) SendEmailForSignUp(c *gin.Context) {
	var req domain.SendEmailForSignUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": pkg.FormatValidationErr(err)})
		return
	}
	myErr := uc.users.SendVerifyMail(c, req.Email, true)
	c.JSON(myErr.Code, myErr.ToGin())
}

func (uc *UsersController) SendEmailForForgetPassword(c *gin.Context) {
	var req domain.SendEmailForForgetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": pkg.FormatValidationErr(err)})
		return
	}
	myErr := uc.users.CheckEmailExists(c, req.Email)
	if myErr.Code >= 400 {
		c.JSON(myErr.Code, myErr.ToGin())
		return
	}
	myErr = uc.users.SendVerifyMail(c, req.Email, false)
	c.JSON(myErr.Code, myErr.ToGin())
}

func (uc *UsersController) SignUp(c *gin.Context) {
	var req domain.SignUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": pkg.FormatValidationErr(err)})
		return
	}

	user, myErr := uc.users.Register(c, req.Email, req.Username, req.Password, req.Code)
	if myErr.Code >= 400 {
		c.JSON(myErr.Code, myErr.ToGin())
		return
	}
	// TODO: 这一步出错 但是用户已经创建了，这样再点击创建用户会冲突报错，需要处理
	accessToken, err := tokenutil.CreateAccessToken(user, uc.AccessTokenSecret, uc.AccessTokenExpire)
	if err != nil {
		c.JSON(pkg.CodeInternalServerError, gin.H{"kind": "http request", "msg": "Create accessToken failed"})
		return
	}
	refreshToken, err := tokenutil.CreateRefreshToken(user, uc.RefreshTokenSecret, uc.RefreshTokenExpire)
	if err != nil {
		c.JSON(pkg.CodeInternalServerError, gin.H{"kind": "http request", "msg": "Create refreshToken failed"})
		return
	}
	c.JSON(pkg.CodeCreated, domain.LoginOrSignUpResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (uc *UsersController) ForgetPassword(c *gin.Context) {
	var req domain.ForgetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": pkg.FormatValidationErr(err)})
		return
	}

	myErr := uc.users.ForgetPassword(c, req.Email, req.Code, req.Password)
	c.JSON(myErr.Code, myErr.ToGin())
}

func (uc *UsersController) Login(c *gin.Context) {
	var req domain.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"kind": "http request", "msg": pkg.FormatValidationErr(err)})
		return
	}

	user, myErr := uc.users.Authenticate(c, req.Email, req.Password)
	if myErr.Code >= 400 {
		c.JSON(myErr.Code, myErr.ToGin())
		return
	}
	accessToken, err := tokenutil.CreateAccessToken(user, uc.AccessTokenSecret, uc.AccessTokenExpire)
	if err != nil {
		c.JSON(pkg.CodeInternalServerError, gin.H{"kind": "http request", "msg": "Create accessToken failed"})
		return
	}
	refreshToken, err := tokenutil.CreateRefreshToken(user, uc.RefreshTokenSecret, uc.RefreshTokenExpire)
	if err != nil {
		c.JSON(pkg.CodeInternalServerError, gin.H{"kind": "http request", "msg": "Create refreshToken failed"})
		return
	}
	c.JSON(pkg.CodeGetSuccess, domain.LoginOrSignUpResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (uc *UsersController) GetProfile(c *gin.Context) {
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"msg": "User ID not found"})
		return
	}

	user, myErr := uc.users.GetProfile(c, uid)
	if myErr.Code >= 400 {
		c.JSON(myErr.Code, myErr.ToGin())
		return
	}
	c.JSON(myErr.Code, domain.BuildShowProfileResponse(user))
}

// UpdateSummary 手动触发"画像增量更新"：投递任务后立即返回。
func (uc *UsersController) UpdateSummary(c *gin.Context) {
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"msg": "User ID not found"})
		return
	}

	myErr := uc.users.EnqueueIncrementalProfileUpdate(c, uid)
	c.JSON(myErr.Code, myErr.ToGin())
}

// GenerateSummary 手动触发"画像重新生成"：投递任务后立即返回。
func (uc *UsersController) GenerateSummary(c *gin.Context) {
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"msg": "User ID not found"})
		return
	}

	myErr := uc.users.EnqueueProfileRegeneration(c, uid)
	c.JSON(myErr.Code, myErr.ToGin())
}

// UpdateInfo 改资料：整笔同步完成后才返回"更新成功"。
func (uc *UsersController) UpdateInfo(c *gin.Context) {
	var req domain.InfoRequest
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"msg": "User ID not found"})
		return
	}
	// 校验请求体
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"msg": pkg.FormatValidationErr(err)})
		return
	}

	myErr := uc.users.UpdateProfile(c, uid, &req)
	c.JSON(myErr.Code, myErr.ToGin())
}

func (uc *UsersController) FillInfo(c *gin.Context) {
	var req domain.InfoRequest
	uid, ok := getUserID(c)
	if !ok {
		c.JSON(pkg.CodeUnauthorized, gin.H{"msg": "User ID not found"})
		return
	}
	// 校验请求体
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(pkg.CodeBadRequest, gin.H{"msg": pkg.FormatValidationErr(err)})
		return
	}

	myErr := uc.users.FillProfile(c, uid, &req)
	c.JSON(myErr.Code, myErr.ToGin())
}

// getUserID 从 JWT 中间件注入的上下文取出用户 ID。
func getUserID(c *gin.Context) (uint, bool) {
	raw, ok := c.Get("x-user-id")
	if !ok {
		return 0, false
	}
	uid, ok := raw.(uint)
	return uid, ok
}
