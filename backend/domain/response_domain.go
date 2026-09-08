package domain

import (
	"github.com/gin-gonic/gin"
)

func ErrorResponse(message string) gin.H {
	return gin.H{"error": message}
}

func SuccessResponse(message string) gin.H {
	return gin.H{"code": 0, "message": message}
}

type LoginOrSignUpResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type ShowProfileResponse struct {
	Age      int    `json:"age"`
	Gender   string `json:"gender"`
	Job      string `json:"job"`
	Education string `json:"education"`
	Location string `json:"location"`
	Organization string `json:"organization"`
	InterestingFields []string `json:"interestingFields"`
	Summary string `json:"summary"`
}

func BuildShowProfileResponse(user *User) ShowProfileResponse {
	return ShowProfileResponse{
		Age:      user.Age,
		Gender:   user.Gender,
		Job:      user.Job,
		Education: user.Education,
		Location: user.Location,
		Organization: user.Organization,
		InterestingFields: user.InterestingFields,
		Summary: user.Summary,
	}
}

type ItemsDetail struct {
	ID uint `json:"id"`
	Title string `json:"title"`
	WebSource string `json:"web_source"`
	PublishDate string `json:"publish_date"`
	Authors []string `json:"authors"`
	Abstract string `json:"abstract"`
	Fields []string `json:"fields"`
	Keywords []string `json:"keywords"`
	IsChinese bool `json:"is_chinese"`
	TitleChinese string `json:"title_chinese"`
	AbstractChinese string `json:"abstract_chinese"`
	Summary string `json:"summary"`
}