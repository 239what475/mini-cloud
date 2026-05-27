package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/projecttoken"
	"mini-cloud/internal/controlplane/store"
)

type projectTokenHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newProjectTokenHandler(logger *slog.Logger, stores *store.Store) projectTokenHandler {
	return projectTokenHandler{
		logger: logger,
		store:  stores,
	}
}

func (h projectTokenHandler) issueProjectAPIToken(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	var input projecttoken.IssueInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	issued, err := h.store.CreateProjectAPIToken(r.Context(), projectID, input)
	if err != nil {
		switch {
		case errors.Is(err, projecttoken.ErrNameRequired),
			errors.Is(err, projecttoken.ErrNameInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrProjectAPITokenNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("issue control-plane project api token failed", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "project.api_token.issue",
		TargetType: "project_api_token",
		TargetID:   issued.Token.ID,
		TargetName: issued.Token.Name,
		Details: map[string]any{
			"tokenPrefix": issued.Token.TokenPrefix,
		},
	})

	writeJSON(w, http.StatusCreated, issued)
}

func (h projectTokenHandler) listProjectAPITokens(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	if _, err := h.store.GetProject(r.Context(), projectID); err != nil {
		if errors.Is(err, store.ErrProjectNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		}
		logger.Error("check project before listing control-plane api tokens failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	items, err := h.store.ListProjectAPITokens(r.Context(), projectID)
	if err != nil {
		logger.Error("list control-plane project api tokens failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h projectTokenHandler) deleteProjectAPIToken(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	tokenID := r.PathValue("tokenID")
	if projectID == "" || tokenID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID and tokenID are required"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	deleted, err := h.store.DeleteProjectAPIToken(r.Context(), projectID, tokenID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectAPITokenNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("delete control-plane project api token failed", "project_id", projectID, "token_id", tokenID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "project.api_token.delete",
		TargetType: "project_api_token",
		TargetID:   deleted.ID,
		TargetName: deleted.Name,
		Details: map[string]any{
			"tokenPrefix": deleted.TokenPrefix,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"token":   deleted,
	})
}
