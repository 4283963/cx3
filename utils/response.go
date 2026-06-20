package utils

import (
	"cx3/model"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	CodeSuccess           = 0
	CodeBadRequest        = 400
	CodeUnauthorized      = 401
	CodeForbidden         = 403
	CodeNotFound          = 404
	CodeTooManyRequests   = 429
	CodeInternalError     = 500
	CodeServiceUnavailable = 503

	CodeStockNotEnough    = 10001
	CodeShelfLocked       = 10002
	CodeShelfOffline      = 10003
	CodeProductNotFound   = 10004
	CodeIdempotentKeyUsed = 10005
	CodeSlotMismatch      = 10006
)

var codeMessages = map[int]string{
	CodeSuccess:            "success",
	CodeBadRequest:         "参数错误",
	CodeUnauthorized:       "未授权",
	CodeForbidden:          "无权限",
	CodeNotFound:           "资源不存在",
	CodeTooManyRequests:    "请求过于频繁",
	CodeInternalError:      "服务器内部错误",
	CodeServiceUnavailable: "服务暂不可用",
	CodeStockNotEnough:     "库存不足",
	CodeShelfLocked:        "货架已锁定",
	CodeShelfOffline:       "货架已离线",
	CodeProductNotFound:    "商品不存在",
	CodeIdempotentKeyUsed:  "幂等键已使用",
	CodeSlotMismatch:       "货道与商品不匹配",
}

func GetMessage(code int) string {
	if msg, ok := codeMessages[code]; ok {
		return msg
	}
	return "未知错误"
}

func Success(c *gin.Context, data interface{}) {
	traceID := GetTraceID(c)
	c.JSON(200, model.Response{
		Code:    CodeSuccess,
		Message: GetMessage(CodeSuccess),
		Data:    data,
		TraceID: traceID,
		Now:     time.Now().UnixMilli(),
	})
}

func Fail(c *gin.Context, httpCode int, code int, message string) {
	if message == "" {
		message = GetMessage(code)
	}
	traceID := GetTraceID(c)
	c.JSON(httpCode, model.Response{
		Code:    code,
		Message: message,
		TraceID: traceID,
		Now:     time.Now().UnixMilli(),
	})
}

func FailWithError(c *gin.Context, code int, err error) {
	Fail(c, 200, code, err.Error())
}
