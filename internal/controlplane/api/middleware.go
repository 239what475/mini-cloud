package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"mini-cloud/internal/logctx"

	"github.com/gin-gonic/gin"
)

func ginRequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := logctx.EnsureRequestID(c.GetHeader(logctx.HeaderRequestID))
		ctx := logctx.WithFields(c.Request.Context(), logctx.Fields{RequestID: requestID})
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set(logctx.HeaderRequestID, requestID)

		start := time.Now()
		c.Next()
		logctx.Logger(ctx, logger).Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"host", c.Request.Host,
			"remote_addr", c.ClientIP(),
			"status_code", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func ginRecoverPanics(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logctx.Logger(c.Request.Context(), logger).Error("panic while handling request",
					"panic", recovered,
					"stack", string(debug.Stack()),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, map[string]any{
					"error": "internal server error",
				})
			}
		}()
		c.Next()
	}
}
