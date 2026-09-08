package controller

import (
	// 自定义包
	"dcpro/domain"
	"dcpro/internal/tokenutil"
	"dcpro/pkg"
	"dcpro/repository"

	// 官方或第三方包
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UsersController struct {
	user_repo *repository.UserRepository
	AccessTokenSecret string
	AccessTokenExpire int
	RefreshTokenSecret string
	RefreshTokenExpire int
}

func NewUsersController(
	user_repo *repository.UserRepository,
	accessTokenSecret string,accessTokenExpire int,
	refreshTokenSecret string,refreshTokenExpire int) *UsersController {
	return &UsersController{
		user_repo: user_repo,
		AccessTokenSecret: accessTokenSecret,
		AccessTokenExpire: accessTokenExpire,
		RefreshTokenSecret: refreshTokenSecret,
		RefreshTokenExpire: refreshTokenExpire,
	}
}

func (uc *UsersController) SignUp(c *gin.Context){
	var req domain.SignUpRequest
	// 创建空对象
	user := &domain.User{}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"error": pkg.FormatValidationErr(err),
		})
		return
	}

	// 判断用户名是否存在
	if isExist, err := uc.user_repo.IsNameExist(c, req.Username); err != nil {
		c.JSON(500, domain.ErrorResponse(err.Error()))
		return
	}else if isExist{
		c.JSON(400, domain.ErrorResponse("Username already exists"))
		return
	}

	if err := uc.user_repo.AddNewUserByReq(c, user, &req); err != nil {
		c.JSON(500, domain.ErrorResponse(err.Error()))
		return
	}else{
		accessToken, err := tokenutil.CreateAccessToken(user, uc.AccessTokenSecret, uc.AccessTokenExpire)
		if err != nil {
			c.JSON(500, domain.ErrorResponse(err.Error()))
			return
		}
		refreshToken, err := tokenutil.CreateRefreshToken(user, uc.RefreshTokenSecret, uc.RefreshTokenExpire)
		if err != nil {
			c.JSON(500, domain.ErrorResponse(err.Error()))
			return
		}
		c.JSON(200, domain.LoginOrSignUpResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
		})
	}
}

func (uc *UsersController) Login(c *gin.Context){
	var req domain.LoginRequest
	user := &domain.User{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"error": pkg.FormatValidationErr(err),
		})
		return
	}

	if err := uc.user_repo.FindUserByName(c, user, req.Username); err != nil {
		c.JSON(400, domain.ErrorResponse(err.Error()))
		return
	}else if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil{
		c.JSON(500, domain.ErrorResponse("Username or Password is incorrect"))
		return
	}else{
		if err := uc.user_repo.ExtendUnameExpire(c, req.Username); err != nil {
			c.JSON(400, domain.ErrorResponse(err.Error()))
			return
		}
		accessToken, err := tokenutil.CreateAccessToken(user, uc.AccessTokenSecret, uc.AccessTokenExpire)
		if err != nil {
			c.JSON(500, domain.ErrorResponse(err.Error()))
			return
		}
		refreshToken, err := tokenutil.CreateRefreshToken(user, uc.RefreshTokenSecret, uc.RefreshTokenExpire)
		if err != nil {
			c.JSON(500, domain.ErrorResponse(err.Error()))
			return
		}
		c.JSON(200, domain.LoginOrSignUpResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
		})
		return
	}
}

func (uc *UsersController) GetProfile(c *gin.Context){
	var resp domain.ShowProfileResponse

	userID, ok := c.Get("x-user-id")
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(400, domain.ErrorResponse("User ID not found"))
		return
	}
	user := &domain.User{}
	if err := uc.user_repo.FindUserByID(c, user, uid); err != nil {
		c.JSON(500, domain.ErrorResponse(err.Error()))
		return
	}else{
		if user.ID == 0{
			c.JSON(400, domain.ErrorResponse("User not found"))
			return
		}
		resp = domain.BuildShowProfileResponse(user)
		c.JSON(200, resp)
		return
	}
}
func (uc *UsersController) UpdateSummary(c *gin.Context){
	userID, ok := c.Get("x-user-id")
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(400, domain.ErrorResponse("User ID not found"))
		return
	}

	err := uc.user_repo.UpdateUserSummary(c, uid)
	
	if err != nil {
		c.JSON(500, gin.H{"msg": "提交任务失败"})
		return
	}

	c.JSON(200, gin.H{
		"msg":    "任务已提交",
		// "taskID": info.ID,
	})
}

func (uc *UsersController) GenerateSummary(c *gin.Context){
	userID, ok := c.Get("x-user-id")
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(400, domain.ErrorResponse("User ID not found"))
		return
	}

	err := uc.user_repo.GenerateUserSummary(c, uid)
	
	if err != nil {
		c.JSON(500, gin.H{"msg": "提交任务失败"})
		return
	}

	c.JSON(200, gin.H{
		"msg":    "任务已提交",
		// "taskID": info.ID,
	})
}

func (uc *UsersController) UpdateInfo(c *gin.Context){
	var req domain.InfoRequest
	userID, ok := c.Get("x-user-id")
	uid, ok := userID.(uint)
	if !ok {
		c.JSON(400, domain.ErrorResponse("User ID not found"))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"error": pkg.FormatValidationErr(err),
		})
		return
	}
	updateMap := switch_info_map(req)
	if len(updateMap) == 0 {
		c.JSON(400, gin.H{"msg": "至少提交一个更改字段"})
		return
	}
	err := uc.user_repo.UpdateUserInfo(c, uid, updateMap)
	if err != nil {
		c.JSON(500, gin.H{"msg": "提交任务失败"})
		return
	}
	c.JSON(200, gin.H{
		"msg":    "任务已提交",
	})
}

// func (uc *UsersController) FillInfo(c *gin.Context){
// 	var req domain.InfoRequest
// 	userID, ok := c.Get("x-user-id")
// 	uid, ok := userID.(uint)
// 	if !ok {
// 		c.JSON(400, domain.ErrorResponse("User ID not found"))
// 		return
// 	}
// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		c.JSON(400, gin.H{
// 			"error": pkg.FormatValidationErr(err),
// 		})
// 		return
// 	}
// 	updateMap := switch_info_map(req)
// 	if len(updateMap) == 0 {
// 		c.JSON(400, gin.H{"msg": "至少提交一个更改字段"})
// 		return
// 	}
// 	err := uc.user_repo.FillUserInfo(c, uid, updateMap)
// 	if err != nil {
// 		c.JSON(500, gin.H{"msg": "提交任务失败"})
// 		return
// 	}
// 	c.JSON(200, gin.H{
// 		"msg":    "任务已提交",
// 	})
// }

func switch_info_map(req domain.InfoRequest) map[string]interface{}{
	updateMap := make(map[string]interface{})
	if req.Age != nil {
		updateMap["age"] = *req.Age
	}
	if req.Gender != nil {
		updateMap["gender"] = *req.Gender
	}
	if req.Job != nil {
		updateMap["job"] = *req.Job
	}
	if req.Education != nil {
		updateMap["education"] = *req.Education
	}
	if req.Location != nil {
		updateMap["location"] = *req.Location
	}
	if req.Organization != nil {
		updateMap["organization"] = *req.Organization
	}
	if req.InterestingFields != nil {
		updateMap["interesting_fields"] = *req.InterestingFields
	}
	return updateMap
}