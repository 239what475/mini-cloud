package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/logctx"

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

type registerPlaneRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	GRPCEndpoint string `json:"grpcEndpoint"`
}

type planeResource struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	DisplayName         string                 `json:"displayName"`
	Provider            string                 `json:"provider"`
	Region              string                 `json:"region"`
	GRPCEndpoint        string                 `json:"grpcEndpoint"`
	CreatedAt           time.Time              `json:"createdAt"`
	Status              planeStatusResource    `json:"status"`
	LatestNodeInventory *nodeInventoryResource `json:"latestNodeInventory,omitempty"`
}

type planeStatusResource struct {
	PlaneID         string     `json:"planeID"`
	Status          string     `json:"status"`
	Message         string     `json:"message"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type nodeInventoryResource struct {
	PlaneID           string    `json:"planeID"`
	ObservedAt        time.Time `json:"observedAt"`
	NodesTotal        int       `json:"nodesTotal"`
	NodesReady        int       `json:"nodesReady"`
	CPUMilliCapacity  int       `json:"cpuMilliCapacity"`
	CPUMilliAllocated int       `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int       `json:"memoryMiCapacity"`
	MemoryMiAllocated int       `json:"memoryMiAllocated"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (h planeHandler) registerPlane(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	var request registerPlaneRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	registered, err := h.store.RegisterPlane(c.Request.Context(), store.RegisterPlaneInput{
		Name:         request.Name,
		DisplayName:  request.DisplayName,
		Provider:     request.Provider,
		Region:       request.Region,
		GRPCEndpoint: request.GRPCEndpoint,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInput):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("register plane failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:  "control.plane.register",
		Message: "registered plane " + registered.Name,
	})

	c.JSON(http.StatusOK, buildPlaneResource(registered))
}

func (h planeHandler) listPlanes(c *gin.Context) {
	logger := logctx.Logger(c.Request.Context(), h.logger)
	items, err := h.store.ListPlanes(c.Request.Context())
	if err != nil {
		logger.Error("list control planes failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	out := make([]planeResource, 0, len(items))
	for _, item := range items {
		out = append(out, buildPlaneResource(item))
	}
	c.JSON(http.StatusOK, map[string]any{"items": out})
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

	c.JSON(http.StatusOK, buildPlaneResource(item))
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
		Action:  "control.plane.delete",
		Message: "deleted plane " + plane.Name,
	})

	c.JSON(http.StatusOK, map[string]any{
		"deleted": true,
		"id":      planeID,
	})
}

func buildPlaneResource(item model.PlaneDetail) planeResource {
	out := planeResource{
		ID:           item.ID,
		Name:         item.Name,
		DisplayName:  item.DisplayName,
		Provider:     item.Provider,
		Region:       item.Region,
		GRPCEndpoint: item.GRPCEndpoint,
		CreatedAt:    item.CreatedAt,
		Status: planeStatusResource{
			PlaneID:         item.Status.PlaneID,
			Status:          item.Status.Status,
			Message:         item.Status.Message,
			LastHeartbeatAt: item.Status.LastHeartbeatAt,
			LastSyncAt:      item.Status.LastSyncAt,
			UpdatedAt:       item.Status.UpdatedAt,
		},
	}
	if item.LatestNodeInventory != nil {
		inventory := item.LatestNodeInventory
		out.LatestNodeInventory = &nodeInventoryResource{
			PlaneID:           inventory.PlaneID,
			ObservedAt:        inventory.ObservedAt,
			NodesTotal:        inventory.NodesTotal,
			NodesReady:        inventory.NodesReady,
			CPUMilliCapacity:  inventory.CPUMilliCapacity,
			CPUMilliAllocated: inventory.CPUMilliAllocated,
			MemoryMiCapacity:  inventory.MemoryMiCapacity,
			MemoryMiAllocated: inventory.MemoryMiAllocated,
			UpdatedAt:         inventory.UpdatedAt,
		}
	}
	return out
}
