package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/runtimepool"
	"mini-cloud/internal/controlplane/store"
)

type controlRuntimePoolHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newControlRuntimePoolHandler(logger *slog.Logger, stores *store.Store) controlRuntimePoolHandler {
	return controlRuntimePoolHandler{
		logger: logger,
		store:  stores,
	}
}

func (h controlRuntimePoolHandler) listRuntimeNodePools(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	views, err := h.listViews(r.Context())
	if err != nil {
		logger.Error("list control runtime node pools failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (h controlRuntimePoolHandler) getRuntimeNodePool(w http.ResponseWriter, r *http.Request) {
	view, err := h.getView(r)
	if err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "get runtime node pool failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h controlRuntimePoolHandler) upsertRuntimeNodePool(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}

	var input runtimepool.UpsertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	plane, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "get plane before upsert runtime node pool failed")
		return
	}

	pool, err := h.store.UpsertRuntimeNodePool(r.Context(), planeID, input)
	if err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "upsert runtime node pool failed")
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.runtime_node_pool.upsert",
		TargetType: "runtime_node_pool",
		TargetID:   planeID,
		TargetName: plane.Name,
		Details: map[string]any{
			"planeID":          planeID,
			"minReady":         pool.MinReady,
			"maxReady":         pool.MaxReady,
			"headroomCPUMilli": pool.HeadroomCPUMilli,
			"headroomMemoryMi": pool.HeadroomMemoryMi,
		},
	})

	writeJSON(w, http.StatusOK, runtimepool.BuildView(pool, plane))
}

func (h controlRuntimePoolHandler) deleteRuntimeNodePool(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}

	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	plane, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "get plane before delete runtime node pool failed")
		return
	}
	pool, err := h.store.GetRuntimeNodePoolByPlane(r.Context(), planeID)
	if err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "get runtime node pool before delete failed")
		return
	}
	if err := h.store.DeleteRuntimeNodePoolByPlane(r.Context(), planeID); err != nil {
		h.writeRuntimeNodePoolError(w, r, err, "delete runtime node pool failed")
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.runtime_node_pool.delete",
		TargetType: "runtime_node_pool",
		TargetID:   planeID,
		TargetName: plane.Name,
		Details: map[string]any{
			"planeID":          planeID,
			"minReady":         pool.MinReady,
			"maxReady":         pool.MaxReady,
			"headroomCPUMilli": pool.HeadroomCPUMilli,
			"headroomMemoryMi": pool.HeadroomMemoryMi,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"planeID": planeID,
	})
}

func (h controlRuntimePoolHandler) getView(r *http.Request) (runtimepool.View, error) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		return runtimepool.View{}, runtimepool.ErrPlaneIDRequired
	}
	plane, err := h.store.GetPlane(r.Context(), planeID)
	if err != nil {
		return runtimepool.View{}, err
	}
	pool, err := h.store.GetRuntimeNodePoolByPlane(r.Context(), planeID)
	if err != nil {
		return runtimepool.View{}, err
	}
	return runtimepool.BuildView(pool, plane), nil
}

func (h controlRuntimePoolHandler) listViews(ctx context.Context) ([]runtimepool.View, error) {
	planes, err := h.store.ListPlanes(ctx)
	if err != nil {
		return nil, err
	}
	pools, err := h.store.ListRuntimeNodePools(ctx)
	if err != nil {
		return nil, err
	}

	planeByID := make(map[string]plane.Detail, len(planes))
	for _, item := range planes {
		planeByID[item.ID] = item
	}

	views := make([]runtimepool.View, 0, len(pools))
	for _, pool := range pools {
		plane, ok := planeByID[pool.PlaneID]
		if !ok {
			continue
		}
		views = append(views, runtimepool.BuildView(pool, plane))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Provider != views[j].Provider {
			return views[i].Provider < views[j].Provider
		}
		if views[i].Region != views[j].Region {
			return views[i].Region < views[j].Region
		}
		if views[i].PlaneName != views[j].PlaneName {
			return views[i].PlaneName < views[j].PlaneName
		}
		return views[i].PlaneID < views[j].PlaneID
	})
	return views, nil
}

func (h controlRuntimePoolHandler) writeRuntimeNodePoolError(w http.ResponseWriter, r *http.Request, err error, message string) {
	logger := requestScopedLogger(r, h.logger)
	switch {
	case errors.Is(err, runtimepool.ErrPlaneIDRequired),
		errors.Is(err, runtimepool.ErrInvalidMinReady),
		errors.Is(err, runtimepool.ErrInvalidMaxReady),
		errors.Is(err, runtimepool.ErrInvalidMaxReadyLessMin),
		errors.Is(err, runtimepool.ErrInvalidHeadroomCPU),
		errors.Is(err, runtimepool.ErrInvalidHeadroomMemory):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	case errors.Is(err, store.ErrPlaneNotFound),
		errors.Is(err, store.ErrRuntimeNodePoolNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	default:
		logger.Error(message, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
	}
}
