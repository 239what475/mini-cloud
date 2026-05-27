package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/common/project"
	"mini-cloud/internal/controlplane/store"
)

type projectHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newProjectHandler(logger *slog.Logger, stores *store.Store) projectHandler {
	return projectHandler{logger: logger, store: stores}
}

func (h projectHandler) createProject(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	var input project.CreateProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	created, err := h.store.CreateProject(r.Context(), input)
	if err != nil {
		switch {
		case isProjectInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create global project failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  created.ID,
		Action:     "project.create",
		TargetType: "project",
		TargetID:   created.ID,
		TargetName: created.Name,
		Details: map[string]any{
			"displayName": created.DisplayName,
			"ownerUserID": created.OwnerUserID,
			"quota":       created.Quota,
		},
	})

	writeJSON(w, http.StatusCreated, created)
}

func (h projectHandler) listProjects(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)
	items, err := h.store.ListProjects(r.Context())
	if err != nil {
		logger.Error("list global projects failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h projectHandler) getProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	item, err := h.store.GetProject(r.Context(), projectID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("get global project failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h projectHandler) updateProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	var input project.UpdateProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	updated, err := h.store.UpdateProject(r.Context(), projectID, input)
	if err != nil {
		switch {
		case isProjectInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("update global project failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  updated.ID,
		Action:     "project.update",
		TargetType: "project",
		TargetID:   updated.ID,
		TargetName: updated.Name,
		Details: map[string]any{
			"displayName": updated.DisplayName,
			"ownerUserID": updated.OwnerUserID,
			"quota":       updated.Quota,
		},
	})

	writeJSON(w, http.StatusOK, updated)
}

func (h projectHandler) transferProjectOwnership(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	var input struct {
		OwnerUserID string `json:"ownerUserID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	updated, err := h.store.TransferProjectOwnership(r.Context(), projectID, input.OwnerUserID)
	if err != nil {
		switch {
		case isProjectInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("transfer global project ownership failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  updated.ID,
		Action:     "project.transfer_ownership",
		TargetType: "project",
		TargetID:   updated.ID,
		TargetName: updated.Name,
		Details: map[string]any{
			"ownerUserID": updated.OwnerUserID,
		},
	})

	writeJSON(w, http.StatusOK, updated)
}

func (h projectHandler) deleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	item, err := h.store.GetProject(r.Context(), projectID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("load global project before delete failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	if err := h.store.DeleteProject(r.Context(), projectID); err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("delete global project failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  item.ID,
		Action:     "project.delete",
		TargetType: "project",
		TargetID:   item.ID,
		TargetName: item.Name,
		Details:    map[string]any{"displayName": item.DisplayName},
	})

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "projectID": item.ID})
}

func isProjectInputError(err error) bool {
	return errors.Is(err, project.ErrInvalidProjectName) ||
		errors.Is(err, project.ErrDisplayNameRequired) ||
		errors.Is(err, project.ErrOwnerUserIDRequired) ||
		errors.Is(err, project.ErrQuotaMaxServicesInvalid) ||
		errors.Is(err, project.ErrQuotaCPUMilliInvalid) ||
		errors.Is(err, project.ErrQuotaMemoryMiInvalid)
}
