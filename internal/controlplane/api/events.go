package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type eventHandler struct {
	logger *slog.Logger
	store  *store.Store
}

type controlEventResource struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

func newEventHandler(logger *slog.Logger, stores *store.Store) eventHandler {
	return eventHandler{
		logger: logger,
		store:  stores,
	}
}

func recordControlEvent(logger *slog.Logger, stores *store.Store, requestCtx context.Context, input store.CreateControlEventInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(requestCtx), 2*time.Second)
	defer cancel()

	if err := stores.CreateControlEvent(ctx, input); err != nil {
		logger.Error("record control event failed", "error", err, "action", input.Action)
	}
}

func (h eventHandler) listControlEvents(c *gin.Context) {
	items, err := h.store.ListRecentControlEvents(c.Request.Context())
	if err != nil {
		h.logger.Error("list control events failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{
			"error": "internal server error",
		})
		return
	}

	out := make([]controlEventResource, 0, len(items))
	for _, item := range items {
		out = append(out, controlEventResource{
			ID:        item.ID,
			Action:    item.Action,
			Message:   item.Message,
			CreatedAt: item.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, map[string]any{"items": out})
}
