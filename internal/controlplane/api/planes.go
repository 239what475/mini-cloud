package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/coordination"

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
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	DisplayName  string              `json:"displayName"`
	Provider     string              `json:"provider"`
	Region       string              `json:"region"`
	GRPCEndpoint string              `json:"grpcEndpoint"`
	Status       planeStatusResource `json:"status"`
}

type planeStatusResource struct {
	PlaneID         string     `json:"planeID"`
	Status          string     `json:"status"`
	Message         string     `json:"message"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
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

func buildPlaneResourceFromSnapshotView(item coordination.PlaneSnapshotView) planeResource {
	out := planeResource{
		ID:           item.Plane.ID,
		Name:         item.Plane.Name,
		DisplayName:  item.Plane.DisplayName,
		Provider:     item.Plane.Provider,
		Region:       item.Plane.Region,
		GRPCEndpoint: item.Plane.GRPCEndpoint,
		Status: planeStatusResource{
			PlaneID:         item.Plane.Status.PlaneID,
			Status:          item.Plane.Status.Status,
			Message:         item.Plane.Status.Message,
			LastHeartbeatAt: item.Plane.Status.LastHeartbeatAt,
			LastSyncAt:      item.Plane.Status.LastSyncAt,
		},
	}
	return out
}
