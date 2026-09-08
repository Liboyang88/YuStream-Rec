package pkg

import (
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"regexp"
)

var pwdReg = regexp.MustCompile(`^[a-zA-Z0-9]{8,20}$`)
var unRegex = regexp.MustCompile(`^[^\s]{2,15}$`)

// 自定义校验函数
func validateUserPassword(fl validator.FieldLevel) bool {
	return pwdReg.MatchString(fl.Field().String())
}

func validateUserUsername(fl validator.FieldLevel) bool {
	return unRegex.MatchString(fl.Field().String())
}


func FormatValidationErr(err error) map[string]string {
	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		return map[string]string{"error": err.Error()}
	}
	res := make(map[string]string)

	for _, e := range errs {
		field := e.Field() // 字段名：Username / Password
		if e.Tag() == "userpwd" {
			res[field] = "格式错误：必须8-20位字母/数字组合"
		} else if e.Tag() == "userun" {
			res[field] = "格式错误：必须2-15长度的字母/数字/中文/特殊字符组合"
		} else if e.Tag() == "required" {
			res[field] = "该字段不能为空"
		}
	}
	return res
}

// 重点：把注册逻辑抽成一个函数！
func InitUserValidator() {
	// 这里就是你问的那一段代码，完全抽出来了
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("userpwd", validateUserPassword)
		_ = v.RegisterValidation("userun", validateUserUsername)
	}
}