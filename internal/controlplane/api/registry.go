package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/eventlog"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type registryCredentialResource struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Server             string    `json:"server"`
	Username           string    `json:"username"`
	PasswordConfigured bool      `json:"passwordConfigured"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type registryHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newRegistryHandler(logger *slog.Logger, stores *store.Store) registryHandler {
	return registryHandler{logger: logger, store: stores}
}

func (h registryHandler) listRegistryCredentials(c *gin.Context) {
	items, err := h.store.ListRegistryCredentials(c.Request.Context())
	if err != nil {
		logctx.Logger(c.Request.Context(), h.logger).Error("list registry credentials failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]registryCredentialResource, 0, len(items))
	for _, item := range items {
		out = append(out, toRegistryCredentialResource(item))
	}
	c.JSON(http.StatusOK, map[string]any{"items": out})
}

func (h registryHandler) createRegistryCredential(c *gin.Context) {
	var input domain.CreateRegistryCredentialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger)
	created, err := h.store.CreateRegistryCredential(c.Request.Context(), input)
	if err != nil {
		switch {
		case domain.IsInvalidInput(err):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, store.ErrRegistryCredentialNameAlreadyExists):
			c.JSON(http.StatusConflict, map[string]any{"error": err.Error()})
		default:
			logger.Error("create registry credential failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordControlEvent(logger, h.store, c.Request.Context(), eventlog.CreateInput{
		Action:     "registry_credential.create",
		TargetType: "registry_credential",
		TargetID:   created.ID,
		TargetName: created.Name,
	})
	c.JSON(http.StatusCreated, toRegistryCredentialResource(created))
}

func toRegistryCredentialResource(item domain.RegistryCredential) registryCredentialResource {
	return registryCredentialResource{
		ID:                 item.ID,
		Name:               item.Name,
		Server:             item.Server,
		Username:           item.Username,
		PasswordConfigured: item.PasswordConfigured,
		CreatedAt:          item.CreatedAt.UTC(),
		UpdatedAt:          item.UpdatedAt.UTC(),
	}
}
