package api

import (
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/common/operationhistory"
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

func (h operationHistoryHandler) listProjectOperations(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}

	logger := requestScopedLogger(r, h.logger)
	limit, ok := parseOperationHistoryLimit(w, r)
	if !ok {
		return
	}

	items, err := h.store.ListProjectOperationEvents(r.Context(), projectID, limit)
	if err != nil {
		logger.Error("list project operation events failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	principal := principalFromRequest(r)
	if principal.Kind == authPrincipalKindProjectToken {
		sanitized := make([]operationhistory.Record, 0, len(items))
		for _, item := range items {
			sanitized = append(sanitized, sanitizeProjectOperationRecord(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": sanitized})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func sanitizeProjectOperationRecord(item operationhistory.Record) operationhistory.Record {
	out := item
	out.RequestPath = ""
	out.Details = sanitizeProjectOperationDetails(item.Details)

	switch item.TargetType {
	case "project_binding", "project_api_token", "service_apply":
		out.TargetID = ""
		out.TargetName = ""
	}
	if item.ActorKind == "project_token" && item.ActorProjectID != "" && item.ActorProjectID != item.ProjectID {
		out.ActorID = ""
		out.ActorLabel = ""
		out.ActorProjectID = ""
	}
	return out
}

func sanitizeProjectOperationDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(details))
	for key, value := range details {
		switch key {
		case "planeID", "selectedPlane", "selectedPlaneID", "tokenPrefix":
			continue
		default:
			out[key] = value
		}
	}
	return out
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
