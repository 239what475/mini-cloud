package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

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

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return strings.TrimSpace(requestID)
}
