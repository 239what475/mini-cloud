package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"mini-cloud/internal/common/logctx"
	planeclient "mini-cloud/internal/controlplane/planeclient"

	"github.com/gin-gonic/gin"
)

func writeJSON(c *gin.Context, statusCode int, value any) {
	c.JSON(statusCode, value)
}

func writePlaneAPIError(c *gin.Context, err error) bool {
	var rpcErr *planeclient.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}

	payload := map[string]any{
		"error": rpcErr.Message,
		"code":  rpcErr.Code,
	}
	writeJSON(c, http.StatusBadGateway, payload)
	return true
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

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

func serveRootJSONOrIndex(logger *slog.Logger, uiDir string, router *gin.Engine) {
	if assetsPath := filepath.Join(uiDir, "assets"); directoryExists(assetsPath) {
		logger.Debug("serving built web assets", "path", assetsPath)
		router.StaticFS("/assets", http.Dir(assetsPath))
	}

	router.GET("/", func(c *gin.Context) {
		indexPath := filepath.Join(uiDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			logger.Debug("serving built web ui", "path", indexPath)
			c.File(indexPath)
			return
		}

		writeJSON(c, http.StatusOK, map[string]any{
			"service":        "mini-cloud-control-plane",
			"ui":             "not_built",
			"healthz":        "/api/healthz",
			"controlMetrics": "/metrics/control",
			"services":       "/api/v1/services",
			"controlPlanes":  "/api/v1/control/planes",
		})
	})
}
