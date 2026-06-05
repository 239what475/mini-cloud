package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/inventory"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planesync"
	"mini-cloud/internal/controlplane/store"
)

type controlHandler struct {
	logger      *slog.Logger
	store       *store.Store
	syncService *planesync.Service
}

func newControlHandler(logger *slog.Logger, stores *store.Store, syncService *planesync.Service) controlHandler {
	return controlHandler{
		logger:      logger,
		store:       stores,
		syncService: syncService,
	}
}

func (h controlHandler) createPlane(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	var input plane.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	created, err := h.store.CreatePlane(r.Context(), input)
	if err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create plane failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.plane.create",
		TargetType: "plane",
		TargetID:   created.ID,
		TargetName: created.Name,
	})

	writeJSON(w, http.StatusCreated, created)
}

func (h controlHandler) listPlanes(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	items, err := h.store.ListPlanes(r.Context())
	if err != nil {
		logger.Error("list control planes failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h controlHandler) inventory(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	items, err := h.store.ListPlanes(r.Context())
	if err != nil {
		logger.Error("build control inventory failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, inventory.Build(items))
}

func (h controlHandler) getPlane(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	item, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	writeJSON(w, http.StatusOK, item)
}

func (h controlHandler) deletePlane(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	plane, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane before delete failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	if err := h.store.DeletePlane(r.Context(), planeID); err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("delete plane failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.plane.delete",
		TargetType: "plane",
		TargetID:   planeID,
		TargetName: plane.Name,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"id":      planeID,
	})
}

func (h controlHandler) registerPlane(w http.ResponseWriter, r *http.Request) {
	if h.syncService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "plane sync service is not configured"})
		return
	}

	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	var input plane.RegisterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), planesync.ManualPlaneSyncTimeout)
	defer cancel()

	result, err := h.syncService.RegisterPlane(requestCtx, planeID, input)
	if err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case planesync.IsSyncFailure(err):
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("register plane failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.plane.register",
		TargetType: "plane",
		TargetID:   result.Plane.ID,
		TargetName: result.Plane.Name,
	})

	writeJSON(w, http.StatusOK, result)
}

func (h controlHandler) syncPlane(w http.ResponseWriter, r *http.Request) {
	if h.syncService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "plane sync service is not configured"})
		return
	}

	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	requestCtx, cancel := context.WithTimeout(r.Context(), planesync.ManualPlaneSyncTimeout)
	defer cancel()

	result, err := h.syncService.SyncPlane(requestCtx, planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, planesync.ErrPlaneNotRegistered):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		case planesync.IsSyncFailure(err):
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("sync plane failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.plane.sync",
		TargetType: "plane",
		TargetID:   result.Plane.ID,
		TargetName: result.Plane.Name,
	})

	writeJSON(w, http.StatusOK, result)
}

func (h controlHandler) updatePlaneOperation(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	var input plane.UpdateOperationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	if _, err := h.store.UpdatePlaneOperation(r.Context(), planeID, input); err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("update plane operation failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	planeDetail, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		logger.Error("reload plane after operation update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.plane.operation.update",
		TargetType: "plane",
		TargetID:   planeID,
		TargetName: planeDetail.Name,
	})

	writeJSON(w, http.StatusOK, planeDetail)
}

func isPlaneInputError(err error) bool {
	return errors.Is(err, plane.ErrPlaneNameRequired) ||
		errors.Is(err, plane.ErrInvalidPlaneName) ||
		errors.Is(err, plane.ErrPlaneDisplayNameRequired) ||
		errors.Is(err, plane.ErrPlaneProviderRequired) ||
		errors.Is(err, plane.ErrPlaneRegionRequired) ||
		errors.Is(err, plane.ErrPlaneGRPCEndpointRequired) ||
		errors.Is(err, plane.ErrPlaneSouthboundTokenRequired) ||
		errors.Is(err, plane.ErrInvalidPlaneGRPCEndpoint) ||
		errors.Is(err, plane.ErrInvalidPlaneStatus) ||
		errors.Is(err, plane.ErrInvalidPlaneOperationState) ||
		errors.Is(err, plane.ErrPlaneOperationReasonRequired) ||
		errors.Is(err, plane.ErrInvalidNodesTotal) ||
		errors.Is(err, plane.ErrInvalidNodesReady) ||
		errors.Is(err, plane.ErrInvalidNodesReadyExceedsTotal) ||
		errors.Is(err, plane.ErrInvalidCPUMilliCapacity) ||
		errors.Is(err, plane.ErrInvalidCPUMilliAllocated) ||
		errors.Is(err, plane.ErrInvalidCPUMilliAllocation) ||
		errors.Is(err, plane.ErrInvalidMemoryMiCapacity) ||
		errors.Is(err, plane.ErrInvalidMemoryMiAllocated) ||
		errors.Is(err, plane.ErrInvalidMemoryMiAllocation)
}
