package api

import (
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type operationHistoryHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newOperationHistoryHandler(logger *slog.Logger, stores *store.Store) operationHistoryHandler {
	return operationHistoryHandler{
		logger: logger,
		store:  stores,
	}
}

func (h operationHistoryHandler) listControlOperations(c *gin.Context) {
	logger := requestScopedLogger(c, h.logger)
	limit, ok := parseOperationHistoryLimit(c)
	if !ok {
		return
	}

	items, err := h.store.ListControlOperationEvents(c.Request.Context(), limit)
	if err != nil {
		logger.Error("list control operation events failed", "error", err)
		writeJSON(c, http.StatusInternalServerError, map[string]any{
			"error": "internal server error",
		})
		return
	}

	writeJSON(c, http.StatusOK, map[string]any{
		"items": items,
	})
}

func parseOperationHistoryLimit(c *gin.Context) (int, bool) {
	limit, err := httpx.ParseBoundedPositiveIntQuery(c.Request, "limit", 40, 200)
	if err != nil {
		writeJSON(c, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return 0, false
	}
	return limit, true
}
