package pkg

import (
	"errors"

	"github.com/gin-gonic/gin"
)

const(
	CodeGetSuccess = 200 // 成功返回数据
	CodeCreated = 201 // 成功创建数据
	CodeAccepted = 202 // 成功接受请求，尚未处理完
	CodeNoContent = 204 // 成功，且无返回数据

	
	CodeBadRequest = 400 // 请求参数错误
	CodeUnauthorized = 401 // 未授权
	CodeForbidden = 403 // 权限拒绝，该登录用户无权限访问该资源或执行该操作
	CodeNotFound = 404 // 资源不存在
	CodeMethodNotAllowed = 405 // 方法不允许，不支持该请求方法（接口只支持 GET，你发 POST）
	CodeTimeout = 408 // 请求超时
	CodeConflict = 409 // 冲突，资源已存在
	CodeRateLimited          = 429 // 请求过于频繁
	CodeRequestEntityTooLarge = 413 // 请求体过大
	
	CodeInternalServerError = 500 // 服务器未知错误（服务异常）
)

// ErrSummaryNotFound 用户摘要不存在
var ErrSummaryNotFound = errors.New("该用户的摘要不存在")
var ErrPasswordEmpty = errors.New("搜索到密码为空")
var ErrTopicsNotFound = errors.New("该用户的主题不存在")
// var ErrUserNameTaken = errors.New("用户名已存在")
var ErrUserEmailTaken = errors.New("该邮箱已关联其他账号")
var ErrUserCacheInit = errors.New("初始化用户缓存失败")
var ErrLLMSummaryEmpty = errors.New("LLM 返回的 summary 为空")
var ErrLLMTopicsEmpty = errors.New("LLM 返回的 interest_topics 为空")



// MyError 自定义错误结构体
type MyError struct {
	Code int
	Kind string // 业务类型
	Msg  string
}

func (e *MyError) ToGin() gin.H {
	return gin.H{"kind": e.Kind, "msg": e.Msg}
}