package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/store"
)

type controlDeployHandler struct {
	logger *slog.Logger
	store  *store.Store
	svc    *deploy.Service
}

func newControlDeployHandler(logger *slog.Logger, stores *store.Store, svc *deploy.Service) controlDeployHandler {
	return controlDeployHandler{
		logger: logger,
		store:  stores,
		svc:    svc,
	}
}

type targetedServiceApplyResponse struct {
	PlaneID           string `json:"planeID"`
	Action            string `json:"action"`
	DesiredGeneration int64  `json:"desiredGeneration"`
}

func (h controlDeployHandler) applyService(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "deploy service is not configured"})
		return
	}

	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}

	var input deploy.ApplyServiceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	result, err := h.svc.ApplyService(r.Context(), planeID, input)
	if err != nil {
		switch {
		case errors.Is(err, deploy.ErrPlaneIDRequired),
			errors.Is(err, deploy.ErrServiceNameRequired),
			errors.Is(err, deploy.ErrInvalidServiceName),
			errors.Is(err, deploy.ErrDisplayNameRequired),
			errors.Is(err, deploy.ErrRegionRequired),
			errors.Is(err, deploy.ErrInvalidReplicas),
			errors.Is(err, deploy.ErrInvalidInstanceClass),
			errors.Is(err, deploy.ErrImageRequired),
			errors.Is(err, deploy.ErrInvalidDefaultPort),
			errors.Is(err, deploy.ErrInvalidReadinessPath),
			errors.Is(err, deploy.ErrInvalidEnvironmentKey):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound),
			errors.Is(err, store.ErrConfigSetNotFound),
			errors.Is(err, store.ErrSecretSetNotFound),
			errors.Is(err, store.ErrRegistryCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, deploy.ErrPlaneNotRegistered),
			errors.Is(err, deploy.ErrPlaneNotAcceptingNewDeployments):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			if writePlaneAPIError(w, err) {
				return
			}
			logger.Error("apply service to plane failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service.apply",
		TargetType: "service_apply",
		TargetID:   input.Metadata.ID,
		TargetName: input.Metadata.Name,
		Details: map[string]any{
			"planeID":           result.PlaneID,
			"action":            result.Action,
			"serviceID":         input.Metadata.ID,
			"desiredGeneration": result.DesiredGeneration,
		},
	})

	writeJSON(w, http.StatusOK, targetedServiceApplyResponse{
		PlaneID:           result.PlaneID,
		Action:            result.Action,
		DesiredGeneration: result.DesiredGeneration,
	})
}
