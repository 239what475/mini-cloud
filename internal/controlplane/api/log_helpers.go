package api

import (
	"log/slog"

	"mini-cloud/internal/common/logctx"

	"github.com/gin-gonic/gin"
)

func withRequestLogFields(c *gin.Context, fields logctx.Fields) {
	c.Request = c.Request.WithContext(logctx.WithFields(c.Request.Context(), fields))
}

func requestScopedLogger(c *gin.Context, logger *slog.Logger) *slog.Logger {
	return logctx.Logger(c.Request.Context(), logger)
}
