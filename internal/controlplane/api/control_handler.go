package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/incident"
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
		Details: map[string]any{
			"displayName":  created.DisplayName,
			"provider":     created.Provider,
			"region":       created.Region,
			"grpcEndpoint": created.GRPCEndpoint,
		},
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
	pools, err := h.store.ListRuntimeNodePools(r.Context())
	if err != nil {
		logger.Error("list control runtime node pools for inventory failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, inventory.Build(items, pools))
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
		Details: map[string]any{
			"provider": plane.Provider,
			"region":   plane.Region,
		},
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
		Details: map[string]any{
			"provider":        result.ObservedProvider,
			"region":          result.ObservedRegion,
			"healthCheckedAt": result.HealthCheckedAt,
			"syncedAt":        result.SyncedAt,
			"alertsFiring":    result.AlertsFiring,
		},
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
		Details: map[string]any{
			"provider":        result.ObservedProvider,
			"region":          result.ObservedRegion,
			"healthCheckedAt": result.HealthCheckedAt,
			"syncedAt":        result.SyncedAt,
			"alertsFiring":    result.AlertsFiring,
		},
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

	updated, err := h.store.UpdatePlaneOperation(r.Context(), planeID, input)
	if err != nil {
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
		Details: map[string]any{
			"state":            updated.State,
			"reason":           updated.Reason,
			"acceptingNewRuns": updated.AcceptingNewRuns(),
		},
	})

	writeJSON(w, http.StatusOK, planeDetail)
}

func (h controlHandler) recordPlaneCapacitySnapshot(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	var input plane.RecordCapacitySnapshotInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	created, err := h.store.RecordPlaneCapacitySnapshot(r.Context(), planeID, input)
	if err != nil {
		switch {
		case isPlaneInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("record plane capacity snapshot failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.capacity_snapshot.record",
		TargetType: "plane",
		TargetID:   planeID,
		Details: map[string]any{
			"nodesTotal":        created.NodesTotal,
			"nodesReady":        created.NodesReady,
			"servicesTotal":     created.ServicesTotal,
			"runsTotal":         created.RunsTotal,
			"cpuMilliCapacity":  created.CPUMilliCapacity,
			"cpuMilliAllocated": created.CPUMilliAllocated,
			"memoryMiCapacity":  created.MemoryMiCapacity,
			"memoryMiAllocated": created.MemoryMiAllocated,
			"capturedAt":        created.CapturedAt,
		},
	})

	writeJSON(w, http.StatusCreated, created)
}

func (h controlHandler) listPlaneCapacitySnapshots(w http.ResponseWriter, r *http.Request) {
	planeID := r.PathValue("planeID")
	if planeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: planeID})
	logger := requestScopedLogger(r, h.logger)

	limit, ok := parseControlListLimit(w, r, 20)
	if !ok {
		return
	}

	items, err := h.store.ListPlaneCapacitySnapshots(r.Context(), planeID, limit)
	if err != nil {
		logger.Error("list plane capacity snapshots failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h controlHandler) createIncident(w http.ResponseWriter, r *http.Request) {
	var input incident.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: input.PlaneID})
	logger := requestScopedLogger(r, h.logger)

	created, err := h.store.CreateIncident(r.Context(), input)
	if err != nil {
		switch {
		case isIncidentInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create control incident failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.incident.create",
		TargetType: "incident",
		TargetID:   created.ID,
		TargetName: created.Summary,
		Details: map[string]any{
			"planeID":    created.PlaneID,
			"severity":   created.Severity,
			"runbookURL": created.RunbookURL,
		},
	})

	writeJSON(w, http.StatusCreated, created)
}

func (h controlHandler) listIncidents(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	limit, ok := parseControlListLimit(w, r, 40)
	if !ok {
		return
	}

	var state string
	if raw := r.URL.Query().Get("state"); raw != "" {
		state = string(raw)
		if !incident.IsState(state) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "state must be one of open, resolved"})
			return
		}
	}

	items, err := h.store.ListIncidents(r.Context(), incident.ListFilter{
		PlaneID: r.URL.Query().Get("planeID"),
		State:   state,
		Limit:   limit,
	})
	if err != nil {
		logger.Error("list control incidents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h controlHandler) updateIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := r.PathValue("incidentID")
	if incidentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "incidentID is required"})
		return
	}

	var input incident.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	updated, err := h.store.UpdateIncident(r.Context(), incidentID, input)
	if err != nil {
		switch {
		case isIncidentInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrIncidentNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrIncidentAlreadyResolved):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger := requestScopedLogger(r, h.logger)
			logger.With("incident_id", incidentID).Error("update control incident failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	r = withRequestLogFields(r, logctx.Fields{PlaneID: updated.PlaneID})
	logger := requestScopedLogger(r, h.logger)
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.incident.update",
		TargetType: "incident",
		TargetID:   updated.ID,
		TargetName: updated.Summary,
		Details: map[string]any{
			"planeID":    updated.PlaneID,
			"severity":   updated.State,
			"runbookURL": updated.RunbookURL,
		},
	})

	writeJSON(w, http.StatusOK, updated)
}

func (h controlHandler) resolveIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := r.PathValue("incidentID")
	if incidentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "incidentID is required"})
		return
	}

	var input incident.ResolveInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	updated, err := h.store.ResolveIncident(r.Context(), incidentID, input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrIncidentNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrIncidentAlreadyResolved):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger := requestScopedLogger(r, h.logger)
			logger.With("incident_id", incidentID).Error("resolve control incident failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	r = withRequestLogFields(r, logctx.Fields{PlaneID: updated.PlaneID})
	logger := requestScopedLogger(r, h.logger)

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.incident.resolve",
		TargetType: "incident",
		TargetID:   updated.ID,
		TargetName: updated.Summary,
		Details: map[string]any{
			"planeID":    updated.PlaneID,
			"severity":   updated.State,
			"resolution": updated.Resolution,
		},
	})

	writeJSON(w, http.StatusOK, updated)
}

func parseControlListLimit(w http.ResponseWriter, r *http.Request, defaultValue int) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultValue, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be a positive integer"})
		return 0, false
	}
	if limit > 200 {
		limit = 200
	}
	return limit, true
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
		errors.Is(err, plane.ErrInvalidServicesTotal) ||
		errors.Is(err, plane.ErrInvalidRunsTotal) ||
		errors.Is(err, plane.ErrInvalidCPUMilliCapacity) ||
		errors.Is(err, plane.ErrInvalidCPUMilliAllocated) ||
		errors.Is(err, plane.ErrInvalidCPUMilliAllocation) ||
		errors.Is(err, plane.ErrInvalidMemoryMiCapacity) ||
		errors.Is(err, plane.ErrInvalidMemoryMiAllocated) ||
		errors.Is(err, plane.ErrInvalidMemoryMiAllocation)
}

func isIncidentInputError(err error) bool {
	return errors.Is(err, incident.ErrPlaneIDRequired) ||
		errors.Is(err, incident.ErrInvalidSeverity) ||
		errors.Is(err, incident.ErrIncidentSummaryNeeded) ||
		errors.Is(err, incident.ErrInvalidRunbookURL)
}
