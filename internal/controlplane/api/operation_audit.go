package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/store"
)

func recordOperationEvent(logger *slog.Logger, stores *store.Store, r *http.Request, input operationhistory.CreateInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()

	if input.Result == "" {
		input.Result = operationhistory.ResultSucceeded
	}
	if _, err := stores.CreateOperationEvent(ctx, input); err != nil {
		logger.Error("record operation event failed", "error", err, "action", input.Action, "target_type", input.TargetType, "target_id", input.TargetID)
	}
}
