package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type planeHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newPlaneHandler(logger *slog.Logger, stores *store.Store) planeHandler {
	return planeHandler{
		logger: logger,
		store:  stores,
	}
}

func (h planeHandler) createPlane(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	var input store.CreatePlaneInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	created, err := h.store.CreatePlane(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInput):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNameAlreadyExists):
			c.JSON(http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create plane failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:     "control.plane.create",
		TargetType: "plane",
		TargetID:   created.ID,
		TargetName: created.Name,
	})

	c.JSON(http.StatusCreated, created)
}

func (h planeHandler) listPlanes(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	items, err := h.store.ListPlanes(c.Request.Context())
	if err != nil {
		logger.Error("list control planes failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, map[string]any{"items": items})
}

func (h planeHandler) inventory(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	items, err := h.store.ListPlanes(c.Request.Context())
	if err != nil {
		logger.Error("build control inventory failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, buildInventoryView(items))
}

func (h planeHandler) getPlane(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger).With("plane_id", planeID)

	item, err := h.store.GetPlane(c.Request.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	c.JSON(http.StatusOK, item)
}

func (h planeHandler) deletePlane(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger).With("plane_id", planeID)

	plane, err := h.store.GetPlane(c.Request.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane before delete failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	if err := h.store.DeletePlane(c.Request.Context(), planeID); err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("delete plane failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:     "control.plane.delete",
		TargetType: "plane",
		TargetID:   planeID,
		TargetName: plane.Name,
	})

	c.JSON(http.StatusOK, map[string]any{
		"deleted": true,
		"id":      planeID,
	})
}
