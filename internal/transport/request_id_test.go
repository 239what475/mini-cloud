package transport

import (
	"context"
	"testing"
)

func TestEnsureRequestID(t *testing.T) {
	t.Parallel()

	if got := EnsureRequestID(" request-a "); got != "request-a" {
		t.Fatalf("EnsureRequestID returned %q, want request-a", got)
	}
	if got := EnsureRequestID(""); got == "" {
		t.Fatal("EnsureRequestID returned empty generated ID")
	}
}

func TestRequestIDContext(t *testing.T) {
	t.Parallel()

	ctx := ContextWithRequestID(context.Background(), " request-a ")
	if got := RequestIDFromContext(ctx); got != "request-a" {
		t.Fatalf("RequestIDFromContext returned %q, want request-a", got)
	}
}
