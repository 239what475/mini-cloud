package logctx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
)

const HeaderRequestID = "X-Request-ID"

type Fields struct {
	PlaneID      string
	RequestID    string
	ServiceID    string
	DeploymentID string
	PlanID       string
	NodeID       string
	ExecutionID  string
}

type fieldsKey struct{}

func WithFields(ctx context.Context, update Fields) context.Context {
	current := FieldsFromContext(ctx)
	merged := current.merge(update)
	if merged == current {
		return ctx
	}
	return context.WithValue(ctx, fieldsKey{}, merged)
}

func FieldsFromContext(ctx context.Context) Fields {
	if ctx == nil {
		return Fields{}
	}
	fields, ok := ctx.Value(fieldsKey{}).(Fields)
	if !ok {
		return Fields{}
	}
	return fields
}

func RequestID(ctx context.Context) string {
	return FieldsFromContext(ctx).RequestID
}

func EnsureRequestID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}

	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return "request-id-unavailable"
}

func Logger(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	return WithLoggerFields(fallback, FieldsFromContext(ctx))
}

func WithLoggerFields(logger *slog.Logger, fields Fields) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	attrs := fields.Attrs()
	if len(attrs) == 0 {
		return logger
	}
	return logger.With(attrs...)
}

func (f Fields) Attrs() []any {
	out := make([]any, 0, 16)
	appendIfNotEmpty := func(key string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		out = append(out, key, value)
	}

	appendIfNotEmpty("plane_id", f.PlaneID)
	appendIfNotEmpty("request_id", f.RequestID)
	appendIfNotEmpty("service_id", f.ServiceID)
	appendIfNotEmpty("deployment_id", f.DeploymentID)
	appendIfNotEmpty("plan_id", f.PlanID)
	appendIfNotEmpty("node_id", f.NodeID)
	appendIfNotEmpty("execution_id", f.ExecutionID)
	return out
}

func (f Fields) merge(update Fields) Fields {
	if value := strings.TrimSpace(update.PlaneID); value != "" {
		f.PlaneID = value
	}
	if value := strings.TrimSpace(update.RequestID); value != "" {
		f.RequestID = value
	}
	if value := strings.TrimSpace(update.ServiceID); value != "" {
		f.ServiceID = value
	}
	if value := strings.TrimSpace(update.DeploymentID); value != "" {
		f.DeploymentID = value
	}
	if value := strings.TrimSpace(update.PlanID); value != "" {
		f.PlanID = value
	}
	if value := strings.TrimSpace(update.NodeID); value != "" {
		f.NodeID = value
	}
	if value := strings.TrimSpace(update.ExecutionID); value != "" {
		f.ExecutionID = value
	}
	return f
}
