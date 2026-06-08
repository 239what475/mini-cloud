package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type controlPlaneSelectionHandler struct {
	logger *slog.Logger
	store  *store.Store
	svc    *planeselector.Selector
}

func newControlPlaneSelectionHandler(logger *slog.Logger, stores *store.Store, svc *planeselector.Selector) controlPlaneSelectionHandler {
	return controlPlaneSelectionHandler{
		logger: logger,
		store:  stores,
		svc:    svc,
	}
}

func (h controlPlaneSelectionHandler) previewSelection(c *gin.Context) {
	if h.svc == nil {
		writeJSON(c, http.StatusServiceUnavailable, map[string]any{"error": "plane selector is not configured"})
		return
	}

	logger := requestScopedLogger(c, h.logger)

	var input planeselector.SelectionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	result, err := h.svc.PreviewSelection(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, planeselector.ErrProviderRequired),
			errors.Is(err, planeselector.ErrRegionRequired),
			errors.Is(err, deploy.ErrInvalidInstanceClass):
			writeJSON(c, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("preview selection failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "control.planeselector.preview",
		TargetType: "plane_selection",
		TargetID:   input.Provider + ":" + input.Region,
		TargetName: input.Provider + "/" + input.Region,
	})

	writeJSON(c, http.StatusOK, result)
}
