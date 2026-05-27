package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/projectresource"
	"mini-cloud/internal/controlplane/store"
)

type configSetResource struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"projectID"`
	Name      string            `json:"name"`
	Values    map[string]string `json:"values"`
	CreatedAt string            `json:"createdAt"`
	UpdatedAt string            `json:"updatedAt"`
}

type secretSetResource struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"projectID"`
	Name      string   `json:"name"`
	Keys      []string `json:"keys"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

type registryCredentialResource struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"projectID"`
	Name               string `json:"name"`
	Server             string `json:"server"`
	Username           string `json:"username"`
	PasswordConfigured bool   `json:"passwordConfigured"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type projectResourceHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newProjectResourceHandler(logger *slog.Logger, stores *store.Store) projectResourceHandler {
	return projectResourceHandler{logger: logger, store: stores}
}

func (h projectResourceHandler) listConfigSets(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	items, err := h.store.ListProjectConfigSets(r.Context(), projectID)
	if err != nil {
		requestScopedLogger(r, h.logger).Error("list project config sets failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]configSetResource, 0, len(items))
	for _, item := range items {
		out = append(out, toConfigSetResource(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h projectResourceHandler) createConfigSet(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	var input projectresource.CreateConfigSetInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)
	created, err := h.store.CreateProjectConfigSet(r.Context(), projectID, input)
	if err != nil {
		switch {
		case isProjectResourceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectConfigSetNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create project config set failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "project.config_set.create",
		TargetType: "config_set",
		TargetID:   created.ID,
		TargetName: created.Name,
		Details: map[string]any{
			"keyCount": len(created.Values),
		},
	})
	writeJSON(w, http.StatusCreated, toConfigSetResource(created))
}

func (h projectResourceHandler) listSecretSets(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	items, err := h.store.ListProjectSecretSets(r.Context(), projectID)
	if err != nil {
		requestScopedLogger(r, h.logger).Error("list project secret sets failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]secretSetResource, 0, len(items))
	for _, item := range items {
		out = append(out, toSecretSetResource(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h projectResourceHandler) createSecretSet(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	var input projectresource.CreateSecretSetInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)
	created, err := h.store.CreateProjectSecretSet(r.Context(), projectID, input)
	if err != nil {
		switch {
		case isProjectResourceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectSecretSetNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create project secret set failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "project.secret_set.create",
		TargetType: "secret_set",
		TargetID:   created.ID,
		TargetName: created.Name,
		Details: map[string]any{
			"keyCount": len(created.Keys),
		},
	})
	writeJSON(w, http.StatusCreated, toSecretSetResource(created))
}

func (h projectResourceHandler) listRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	items, err := h.store.ListProjectRegistryCredentials(r.Context(), projectID)
	if err != nil {
		requestScopedLogger(r, h.logger).Error("list project registry credentials failed", "project_id", projectID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]registryCredentialResource, 0, len(items))
	for _, item := range items {
		out = append(out, toRegistryCredentialResource(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h projectResourceHandler) createRegistryCredential(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	var input projectresource.CreateRegistryCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)
	created, err := h.store.CreateProjectRegistryCredential(r.Context(), projectID, input)
	if err != nil {
		switch {
		case isProjectResourceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrProjectRegistryCredentialNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create project registry credential failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "project.registry_credential.create",
		TargetType: "registry_credential",
		TargetID:   created.ID,
		TargetName: created.Name,
		Details: map[string]any{
			"server":   created.Server,
			"username": created.Username,
		},
	})
	writeJSON(w, http.StatusCreated, toRegistryCredentialResource(created))
}

func toConfigSetResource(item projectresource.ConfigSet) configSetResource {
	return configSetResource{
		ID:        item.ID,
		ProjectID: item.ProjectID,
		Name:      item.Name,
		Values:    cloneStringMap(item.Values),
		CreatedAt: item.CreatedAt.UTC().Format(http.TimeFormat),
		UpdatedAt: item.UpdatedAt.UTC().Format(http.TimeFormat),
	}
}

func toSecretSetResource(item projectresource.SecretSet) secretSetResource {
	return secretSetResource{
		ID:        item.ID,
		ProjectID: item.ProjectID,
		Name:      item.Name,
		Keys:      append([]string(nil), item.Keys...),
		CreatedAt: item.CreatedAt.UTC().Format(http.TimeFormat),
		UpdatedAt: item.UpdatedAt.UTC().Format(http.TimeFormat),
	}
}

func toRegistryCredentialResource(item projectresource.RegistryCredential) registryCredentialResource {
	return registryCredentialResource{
		ID:                 item.ID,
		ProjectID:          item.ProjectID,
		Name:               item.Name,
		Server:             item.Server,
		Username:           item.Username,
		PasswordConfigured: item.PasswordConfigured,
		CreatedAt:          item.CreatedAt.UTC().Format(http.TimeFormat),
		UpdatedAt:          item.UpdatedAt.UTC().Format(http.TimeFormat),
	}
}

func isProjectResourceInputError(err error) bool {
	return errors.Is(err, projectresource.ErrConfigSetNameRequired) ||
		errors.Is(err, projectresource.ErrConfigSetNameInvalid) ||
		errors.Is(err, projectresource.ErrConfigSetValuesRequired) ||
		errors.Is(err, projectresource.ErrConfigSetValueKeyInvalid) ||
		errors.Is(err, projectresource.ErrSecretSetNameRequired) ||
		errors.Is(err, projectresource.ErrSecretSetNameInvalid) ||
		errors.Is(err, projectresource.ErrSecretSetValuesRequired) ||
		errors.Is(err, projectresource.ErrSecretSetValueKeyInvalid) ||
		errors.Is(err, projectresource.ErrRegistryCredentialNameRequired) ||
		errors.Is(err, projectresource.ErrRegistryCredentialNameInvalid) ||
		errors.Is(err, projectresource.ErrRegistryCredentialServerRequired) ||
		errors.Is(err, projectresource.ErrRegistryCredentialUsernameRequired) ||
		errors.Is(err, projectresource.ErrRegistryCredentialPasswordRequired)
}
