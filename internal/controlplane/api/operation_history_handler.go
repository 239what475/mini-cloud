package api

import (
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/controlplane/store"
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

func (h operationHistoryHandler) listControlOperations(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	limit, ok := parseOperationHistoryLimit(w, r)
	if !ok {
		return
	}

	items, err := h.store.ListControlOperationEvents(r.Context(), limit)
	if err != nil {
		logger.Error("list control operation events failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal server error",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func parseOperationHistoryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit, err := httpx.ParseBoundedPositiveIntQuery(r, "limit", 40, 200)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return 0, false
	}
	return limit, true
}
