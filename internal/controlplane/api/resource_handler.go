package api

import (
	"errors"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/resource"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type registryCredentialResource struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Server             string `json:"server"`
	Username           string `json:"username"`
	PasswordConfigured bool   `json:"passwordConfigured"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type resourceHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newResourceHandler(logger *slog.Logger, stores *store.Store) resourceHandler {
	return resourceHandler{logger: logger, store: stores}
}

func (h resourceHandler) listRegistryCredentials(c *gin.Context) {
	items, err := h.store.ListRegistryCredentials(c.Request.Context())
	if err != nil {
		requestScopedLogger(c, h.logger).Error("list registry credentials failed", "error", err)
		writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]registryCredentialResource, 0, len(items))
	for _, item := range items {
		out = append(out, toRegistryCredentialResource(item))
	}
	writeJSON(c, http.StatusOK, map[string]any{"items": out})
}

func (h resourceHandler) createRegistryCredential(c *gin.Context) {
	var input resource.CreateRegistryCredentialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeJSON(c, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	logger := requestScopedLogger(c, h.logger)
	created, err := h.store.CreateRegistryCredential(c.Request.Context(), input)
	if err != nil {
		switch {
		case isResourceInputError(err):
			writeJSON(c, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrRegistryCredentialNameAlreadyExists):
			writeJSON(c, http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create registry credential failed", "error", err)
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, c, operationhistory.CreateInput{
		Action:     "registry_credential.create",
		TargetType: "registry_credential",
		TargetID:   created.ID,
		TargetName: created.Name,
	})
	writeJSON(c, http.StatusCreated, toRegistryCredentialResource(created))
}

func toRegistryCredentialResource(item resource.RegistryCredential) registryCredentialResource {
	return registryCredentialResource{
		ID:                 item.ID,
		Name:               item.Name,
		Server:             item.Server,
		Username:           item.Username,
		PasswordConfigured: item.PasswordConfigured,
		CreatedAt:          item.CreatedAt.UTC().Format(http.TimeFormat),
		UpdatedAt:          item.UpdatedAt.UTC().Format(http.TimeFormat),
	}
}

func isResourceInputError(err error) bool {
	return errors.Is(err, resource.ErrRegistryCredentialNameRequired) ||
		errors.Is(err, resource.ErrRegistryCredentialNameInvalid) ||
		errors.Is(err, resource.ErrRegistryCredentialServerRequired) ||
		errors.Is(err, resource.ErrRegistryCredentialUsernameRequired) ||
		errors.Is(err, resource.ErrRegistryCredentialPasswordRequired)
}
