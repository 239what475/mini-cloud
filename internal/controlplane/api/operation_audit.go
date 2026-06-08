package api

import (
	"context"
	"log/slog"
	"time"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

func recordOperationEvent(logger *slog.Logger, stores *store.Store, c *gin.Context, input operationhistory.CreateInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
	defer cancel()

	if input.Result == "" {
		input.Result = operationhistory.ResultSucceeded
	}
	if _, err := stores.CreateOperationEvent(ctx, input); err != nil {
		logger.Error("record operation event failed", "error", err, "action", input.Action, "target_type", input.TargetType, "target_id", input.TargetID)
	}
}
