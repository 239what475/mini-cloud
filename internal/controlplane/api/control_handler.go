package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	domain "mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/inventory"
	"mini-cloud/internal/controlplane/planesync"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type controlHandler struct {
	logger *slog.Logger
	store  *store.Store
	syncer *planesync.Syncer
}

func newControlHandler(logger *slog.Logger, stores *store.Store, syncer *planesync.Syncer) controlHandler {
	return controlHandler{
		logger: logger,
		store:  stores,
		syncer: syncer,
	}
}

func (h controlHandler) createPlane(c *gin.Context) {
	logger := requestScopedLogger(c, h.logger)
	var input domain.PlaneCreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	created, err := h.store.CreatePlane(c.Request.Context(), input)
	if err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(c, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNameAlreadyExists):
			writeJSON(c, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create plane failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "control.plane.create",
		TargetType: "plane",
		TargetID:   created.ID,
		TargetName: created.Name,
	})

	writeJSON(c, http.StatusCreated, created)
}

func (h controlHandler) listPlanes(c *gin.Context) {
	logger := requestScopedLogger(c, h.logger)
	items, err := h.store.ListPlanes(c.Request.Context())
	if err != nil {
		logger.Error("list control planes failed", "error", err)
		writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(c, http.StatusOK, map[string]any{"items": items})
}

func (h controlHandler) inventory(c *gin.Context) {
	logger := requestScopedLogger(c, h.logger)
	items, err := h.store.ListPlanes(c.Request.Context())
	if err != nil {
		logger.Error("build control inventory failed", "error", err)
		writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	writeJSON(c, http.StatusOK, inventory.Build(items))
}

func (h controlHandler) getPlane(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	withRequestLogFields(c, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(c, h.logger)

	item, err := h.store.GetPlane(c.Request.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(c, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	writeJSON(c, http.StatusOK, item)
}

func (h controlHandler) deletePlane(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	withRequestLogFields(c, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(c, h.logger)

	plane, err := h.store.GetPlane(c.Request.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(c, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane before delete failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	if err := h.store.DeletePlane(c.Request.Context(), planeID); err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(c, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("delete plane failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "control.plane.delete",
		TargetType: "plane",
		TargetID:   planeID,
		TargetName: plane.Name,
	})

	writeJSON(c, http.StatusOK, map[string]any{
		"deleted": true,
		"id":      planeID,
	})
}

func (h controlHandler) syncPlane(c *gin.Context) {
	if h.syncer == nil {
		writeJSON(c, http.StatusServiceUnavailable, map[string]any{"error": "plane sync service is not configured"})
		return
	}

	planeID := c.Param("planeID")
	if planeID == "" {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	withRequestLogFields(c, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(c, h.logger)

	requestCtx, cancel := context.WithTimeout(c.Request.Context(), planesync.ManualPlaneSyncTimeout)
	defer cancel()

	result, err := h.syncer.SyncPlane(requestCtx, planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(c, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, planesync.ErrPlaneNotRegistered):
			writeJSON(c, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		case planesync.IsSyncFailure(err):
			writeJSON(c, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("sync plane failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "control.plane.sync",
		TargetType: "plane",
		TargetID:   result.Plane.ID,
		TargetName: result.Plane.Name,
	})

	writeJSON(c, http.StatusOK, result)
}

func (h controlHandler) updatePlaneOperation(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	withRequestLogFields(c, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(c, h.logger)

	var input domain.PlaneUpdateOperationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	if _, err := h.store.UpdatePlaneOperation(c.Request.Context(), planeID, input); err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(c, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(c, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("update plane operation failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	planeDetail, err := h.store.GetPlane(c.Request.Context(), planeID)
	if err != nil {
		logger.Error("reload plane after operation update failed", "error", err)
		writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "control.plane.operation.update",
		TargetType: "plane",
		TargetID:   planeID,
		TargetName: planeDetail.Name,
	})

	writeJSON(c, http.StatusOK, planeDetail)
}

func isPlaneInputError(err error) bool {
	return errors.Is(err, domain.ErrPlaneNameRequired) ||
		errors.Is(err, domain.ErrInvalidPlaneName) ||
		errors.Is(err, domain.ErrPlaneDisplayNameRequired) ||
		errors.Is(err, domain.ErrPlaneProviderRequired) ||
		errors.Is(err, domain.ErrPlaneRegionRequired) ||
		errors.Is(err, domain.ErrPlaneGRPCEndpointRequired) ||
		errors.Is(err, domain.ErrPlaneSouthboundTokenRequired) ||
		errors.Is(err, domain.ErrInvalidPlaneGRPCEndpoint) ||
		errors.Is(err, domain.ErrInvalidPlaneStatus) ||
		errors.Is(err, domain.ErrInvalidPlaneOperationState) ||
		errors.Is(err, domain.ErrPlaneOperationReasonRequired) ||
		errors.Is(err, domain.ErrInvalidNodesTotal) ||
		errors.Is(err, domain.ErrInvalidNodesReady) ||
		errors.Is(err, domain.ErrInvalidNodesReadyExceedsTotal) ||
		errors.Is(err, domain.ErrInvalidCPUMilliCapacity) ||
		errors.Is(err, domain.ErrInvalidCPUMilliAllocated) ||
		errors.Is(err, domain.ErrInvalidCPUMilliAllocation) ||
		errors.Is(err, domain.ErrInvalidMemoryMiCapacity) ||
		errors.Is(err, domain.ErrInvalidMemoryMiAllocated) ||
		errors.Is(err, domain.ErrInvalidMemoryMiAllocation)
}
