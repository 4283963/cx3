package middleware

import (
	"cx3/utils"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				traceID := utils.GetTraceID(c)

				utils.SugarLogger.Errorw("panic recovered",
					zap.String("trace_id", traceID),
					zap.Any("panic", err),
					zap.Stack("stack"),
				)

				utils.Fail(c,
					http.StatusInternalServerError,
					utils.CodeInternalError,
					"服务器内部错误，请稍后重试",
				)

				c.Abort()
			}
		}()
		c.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Trace-ID, X-Request-ID")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, X-Trace-ID")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
