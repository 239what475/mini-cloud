package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/common/logquery"
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

func (h logQueryHandler) queryControlLogs(w http.ResponseWriter, r *http.Request) {
	h.queryLogs(w, r, logquery.Filters{})
}

func (h logQueryHandler) queryLogs(w http.ResponseWriter, r *http.Request, forced logquery.Filters) {
	logger := requestScopedLogger(r, h.logger)
	if h.service == nil || !h.service.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "log query backend is not configured",
		})
		return
	}

	input, err := parseLogQueryInput(r, forced)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	result, err := h.service.QueryRange(r.Context(), input)
	if err != nil {
		var inputErr *logquery.InputError
		var backendErr *logquery.BackendError
		switch {
		case errors.Is(err, logquery.ErrNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": err.Error(),
			})
		case errors.As(err, &inputErr):
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": inputErr.Error(),
			})
		case errors.As(err, &backendErr):
			logger.Error("query aggregated logs failed", "error", backendErr.Error(), "query", backendErr.Query, "status_code", backendErr.StatusCode)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": "query aggregated logs failed",
			})
		default:
			logger.Error("query aggregated logs failed", "error", err, "query", result.Query)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": "query aggregated logs failed",
			})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backend":   "loki",
		"query":     result.Query,
		"start":     result.Start,
		"end":       result.End,
		"limit":     result.Limit,
		"direction": result.Direction,
		"items":     result.Items,
	})
}

func parseLogQueryInput(r *http.Request, forced logquery.Filters) (logquery.QueryInput, error) {
	return httpx.ParseLogQueryInput(r, forced)
}
