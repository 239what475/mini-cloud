package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	domain "mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/inventory"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type controlHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newControlHandler(logger *slog.Logger, stores *store.Store) controlHandler {
	return controlHandler{
		logger: logger,
		store:  stores,
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
