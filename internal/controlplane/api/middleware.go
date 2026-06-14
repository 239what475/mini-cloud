package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/transport"

	"github.com/gin-gonic/gin"
)

func ginRequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		requestID := transport.RequestIDFromContext(c.Request.Context())
		logger.With("request_id", requestID).Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"host", c.Request.Host,
			"remote_addr", c.ClientIP(),
			"status_code", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func ginRequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := transport.EnsureRequestID(c.GetHeader(transport.RequestIDHeader))
		ctx := transport.ContextWithRequestID(c.Request.Context(), requestID)
		ctx = coordination.ContextWithTencentCredential(ctx, coordination.TencentCredential{
			SecretID:     firstHeader(c, "X-Scf-Secret-Id", "X-Scf-Secret-ID"),
			SecretKey:    firstHeader(c, "X-Scf-Secret-Key"),
			SessionToken: firstHeader(c, "X-Scf-Session-Token", "X-Scf-Token"),
		})
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set(transport.RequestIDHeader, requestID)
		c.Next()
	}
}

func firstHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}

func ginRecoverPanics(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.With("request_id", transport.RequestIDFromContext(c.Request.Context())).Error("panic while handling request",
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
