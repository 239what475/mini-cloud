package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/store"
)

type controlPlaneSelectionHandler struct {
	logger *slog.Logger
	store  *store.Store
	svc    *planeselector.Service
}

func newControlPlaneSelectionHandler(logger *slog.Logger, stores *store.Store, svc *planeselector.Service) controlPlaneSelectionHandler {
	return controlPlaneSelectionHandler{
		logger: logger,
		store:  stores,
		svc:    svc,
	}
}

func (h controlPlaneSelectionHandler) previewSelection(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "plane selector is not configured"})
		return
	}

	logger := requestScopedLogger(r, h.logger)

	var input planeselector.SelectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	result, err := h.svc.PreviewSelection(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, planeselector.ErrProviderRequired),
			errors.Is(err, planeselector.ErrRegionRequired),
			errors.Is(err, deploy.ErrInvalidInstanceClass):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("preview selection failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.planeselector.preview",
		TargetType: "plane_selection",
		TargetID:   input.Provider + ":" + input.Region,
		TargetName: input.Provider + "/" + input.Region,
	})

	writeJSON(w, http.StatusOK, result)
}
