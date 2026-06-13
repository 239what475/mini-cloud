package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/model"

	"github.com/gin-gonic/gin"
)

type planeHandler struct {
	logger *slog.Logger
	syncer *coordination.PlaneSyncer
}

func newPlaneHandler(logger *slog.Logger, syncer *coordination.PlaneSyncer) planeHandler {
	return planeHandler{
		logger: logger,
		syncer: syncer,
	}
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

func (h planeHandler) listPlanes(c *gin.Context) {
	views, err := h.syncer.ListPlaneSnapshotViews(c.Request.Context(), coordination.RequestPlaneSyncTimeout)
	if err != nil {
		h.logger.Warn("load plane snapshot views failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]planeResource, 0, len(views))
	for _, item := range views {
		out = append(out, buildPlaneResourceFromSnapshotView(item))
	}
	c.JSON(http.StatusOK, map[string]any{"items": out})
}

func (h planeHandler) inventory(c *gin.Context) {
	views, err := h.syncer.ListPlaneSnapshotViews(c.Request.Context(), coordination.RequestPlaneSyncTimeout)
	if err != nil {
		h.logger.Warn("load inventory snapshot views failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, buildInventoryView(views))
}

func (h planeHandler) checkDNS(c *gin.Context) {
	if err := h.syncer.CheckDNS(c.Request.Context()); err != nil {
		h.logger.Warn("check DNS failed", "error", err)
		c.JSON(http.StatusBadGateway, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

func (h planeHandler) getPlane(c *gin.Context) {
	planeID := c.Param("planeID")
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	logger := h.logger.With("plane_id", planeID)

	view, err := h.syncer.GetPlaneSnapshotView(c.Request.Context(), planeID)
	if err != nil {
		switch {
		case errors.Is(err, coordination.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("get plane failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	c.JSON(http.StatusOK, buildPlaneResourceFromSnapshotView(view))
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

func buildPlaneResourceFromSnapshotView(item coordination.PlaneSnapshotView) planeResource {
	out := buildPlaneResource(item.Plane)
	if item.Snapshot == nil {
		return out
	}
	inventory := nodeInventoryFromSnapshot(item.Plane.ID, item.Snapshot)
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
	return out
}
