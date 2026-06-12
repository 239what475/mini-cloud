package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/controlplane/logquery"
	"mini-cloud/internal/logctx"

	"github.com/gin-gonic/gin"
)

type logQueryHandler struct {
	logger  *slog.Logger
	service *logquery.Service
}

func newLogQueryHandler(logger *slog.Logger, service *logquery.Service) logQueryHandler {
	return logQueryHandler{
		logger:  logger,
		service: service,
	}
}

func (h logQueryHandler) queryControlLogs(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "log query backend is not configured",
		})
		return
	}

	input, err := parseLogQueryInput(c.Request)
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
		"backend": "loki",
		"query":   result.Query,
		"start":   result.Start,
		"end":     result.End,
		"items":   result.Items,
	})
}
