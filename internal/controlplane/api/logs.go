package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/logquery"

	"github.com/gin-gonic/gin"
)

type logQueryHandler struct {
	logger  *slog.Logger
	service logquery.Backend
}

func newLogQueryHandler(logger *slog.Logger, service logquery.Backend) logQueryHandler {
	return logQueryHandler{
		logger:  logger,
		service: service,
	}
}

func (h logQueryHandler) queryControlLogs(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	if h.service == nil || !h.service.Configured() {
		c.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "log query backend is not configured",
		})
		return
	}

	input, err := httpx.ParseLogQueryInput(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	result, err := h.service.QueryRange(c.Request.Context(), input)
	if err != nil {
		var inputErr *logquery.InputError
		var backendErr *logquery.BackendError
		switch {
		case errors.Is(err, logquery.ErrNotConfigured):
			c.JSON(http.StatusServiceUnavailable, map[string]any{
				"error": err.Error(),
			})
		case errors.As(err, &inputErr):
			c.JSON(http.StatusBadRequest, map[string]any{
				"error": inputErr.Error(),
			})
		case errors.As(err, &backendErr):
			logger.Error("query aggregated logs failed", "error", backendErr.Error(), "query", backendErr.Query, "status_code", backendErr.StatusCode)
			c.JSON(http.StatusBadGateway, map[string]any{
				"error": "query aggregated logs failed",
			})
		default:
			logger.Error("query aggregated logs failed", "error", err, "query", result.Query)
			c.JSON(http.StatusBadGateway, map[string]any{
				"error": "query aggregated logs failed",
			})
		}
		return
	}

	c.JSON(http.StatusOK, map[string]any{
		"backend":   "loki",
		"query":     result.Query,
		"start":     result.Start,
		"end":       result.End,
		"limit":     result.Limit,
		"direction": result.Direction,
		"items":     result.Items,
	})
}
