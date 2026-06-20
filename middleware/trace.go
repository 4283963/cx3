package middleware

import (
	"cx3/utils"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TraceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := utils.GetTraceID(c)
		c.Header(utils.TraceIDKey, traceID)
		c.Next()
	}
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		method := c.Request.Method
		clientIP := utils.GetClientIP(c)
		traceID := utils.GetTraceID(c)

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		bodySize := c.Writer.Size()

		if len(c.Errors) > 0 {
			utils.SugarLogger.Errorw("request completed with errors",
				zap.String("trace_id", traceID),
				zap.String("method", method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("client_ip", clientIP),
				zap.Int("status", statusCode),
				zap.Int("body_size", bodySize),
				zap.Duration("latency", latency),
				zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()),
			)
			return
		}

		if statusCode >= 500 {
			utils.SugarLogger.Errorw("request completed",
				zap.String("trace_id", traceID),
				zap.String("method", method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("client_ip", clientIP),
				zap.Int("status", statusCode),
				zap.Int("body_size", bodySize),
				zap.Duration("latency", latency),
			)
		} else if statusCode >= 400 {
			utils.SugarLogger.Warnw("request completed",
				zap.String("trace_id", traceID),
				zap.String("method", method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("client_ip", clientIP),
				zap.Int("status", statusCode),
				zap.Int("body_size", bodySize),
				zap.Duration("latency", latency),
			)
		} else {
			utils.SugarLogger.Infow("request completed",
				zap.String("trace_id", traceID),
				zap.String("method", method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("client_ip", clientIP),
				zap.Int("status", statusCode),
				zap.Int("body_size", bodySize),
				zap.Duration("latency", latency),
			)
		}
	}
}
