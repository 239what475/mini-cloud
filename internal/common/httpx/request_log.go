package httpx

import (
	"context"
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/logctx"
)

func WithRequestLogFields(r *http.Request, fields logctx.Fields) *http.Request {
	if r == nil {
		return nil
	}
	ctx := logctx.WithFields(logctx.WithFields(r.Context(), pathLogFields(r)), fields)
	if ctx != r.Context() {
		*r = *r.WithContext(ctx)
	}
	return r
}

func RequestScopedLogger(r *http.Request, logger *slog.Logger) *slog.Logger {
	if r == nil {
		return logctx.Logger(context.Background(), logger)
	}
	return logctx.Logger(WithRequestLogFields(r, logctx.Fields{}).Context(), logger)
}

func pathLogFields(r *http.Request) logctx.Fields {
	if r == nil {
		return logctx.Fields{}
	}
	return logctx.Fields{
		PlaneID:       r.PathValue("planeID"),
		ServiceID:     firstNonEmptyPathValue(r, "serviceID"),
		NodeID:        r.PathValue("nodeID"),
		RuntimeNodeID: r.PathValue("runtimeNodeID"),
		ExecutionID:   r.PathValue("executionID"),
		DeploymentID:  r.PathValue("deploymentID"),
	}
}

func firstNonEmptyPathValue(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := r.PathValue(key); value != "" {
			return value
		}
	}
	return ""
}
