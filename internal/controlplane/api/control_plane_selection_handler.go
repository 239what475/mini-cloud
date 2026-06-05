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

type autoServiceApplyResponse struct {
	Action    string                        `json:"action,omitempty"`
	PlanID    string                        `json:"planID,omitempty"`
	Selection planeselector.SelectionResult `json:"selection"`
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
		Details: map[string]any{
			"provider":      input.Provider,
			"region":        input.Region,
			"pinnedPlaneID": input.PinnedPlaneID,
			"instanceClass": input.InstanceClass,
			"selectedPlane": selectedPlaneID(result.Decision),
			"failureReason": result.FailureReason,
		},
	})

	writeJSON(w, http.StatusOK, result)
}

func (h controlPlaneSelectionHandler) applyService(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "plane selector is not configured"})
		return
	}

	var input planeselector.ApplyServiceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	result, err := h.svc.Apply(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, planeselector.ErrProviderRequired),
			errors.Is(err, planeselector.ErrRegionRequired),
			errors.Is(err, deploy.ErrServiceNameRequired),
			errors.Is(err, deploy.ErrInvalidInstanceClass),
			errors.Is(err, deploy.ErrInvalidServiceName),
			errors.Is(err, deploy.ErrDisplayNameRequired),
			errors.Is(err, deploy.ErrRegionRequired),
			errors.Is(err, deploy.ErrImageRequired),
			errors.Is(err, deploy.ErrInvalidDefaultPort),
			errors.Is(err, deploy.ErrInvalidReadinessPath),
			errors.Is(err, deploy.ErrInvalidEnvironmentKey):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, deploy.ErrPlaneNotRegistered),
			errors.Is(err, deploy.ErrPlaneNotAcceptingNewRuns):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			if writePlaneAPIError(w, err) {
				return
			}
			logger.Error("auto apply service failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
	}

	if result.Selection.Decision == nil {
		writeJSON(w, http.StatusConflict, result)
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service.auto_apply",
		TargetType: "service_apply",
		TargetID:   input.Metadata.ID,
		TargetName: input.Metadata.Name,
		Details: map[string]any{
			"provider":        result.Selection.Decision.Provider,
			"region":          result.Selection.Decision.Region,
			"pinnedPlaneID":   input.PinnedPlaneID,
			"selectedPlaneID": result.Selection.Decision.PlaneID,
			"action":          result.Accepted.Action,
			"serviceID":       input.Metadata.ID,
			"planID":          result.Accepted.PlanID,
		},
	})

	writeJSON(w, http.StatusOK, autoServiceApplyResponse{
		Action:    result.Accepted.Action,
		PlanID:    result.Accepted.PlanID,
		Selection: result.Selection,
	})
}

func selectedPlaneID(decision *planeselector.Decision) string {
	if decision == nil {
		return ""
	}
	return decision.PlaneID
}
