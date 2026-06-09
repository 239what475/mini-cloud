package logctx

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestWithFieldsMergesValues(t *testing.T) {
	ctx := WithFields(context.Background(), Fields{
		PlaneID:   "plane-a",
		RequestID: "req-a",
	})
	ctx = WithFields(ctx, Fields{
		RequestID: "req-b",
		ServiceID: "service-a",
	})

	fields := FieldsFromContext(ctx)
	if fields.PlaneID != "plane-a" {
		t.Fatalf("PlaneID = %q, want plane-a", fields.PlaneID)
	}
	if fields.RequestID != "req-b" {
		t.Fatalf("RequestID = %q, want req-b", fields.RequestID)
	}
	if fields.ServiceID != "service-a" {
		t.Fatalf("ServiceID = %q, want service-a", fields.ServiceID)
	}
}

func TestLoggerAddsStructuredFields(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := WithFields(context.Background(), Fields{
		PlaneID:   "plane-a",
		RequestID: "req-a",
		PlanID:    "plan-a",
	})

	logger := Logger(ctx, base)
	if logger == nil {
		t.Fatalf("expected logger")
	}
}

func TestEnsureRequestIDReturnsProvidedValue(t *testing.T) {
	if got := EnsureRequestID("req-a"); got != "req-a" {
		t.Fatalf("EnsureRequestID returned %q, want req-a", got)
	}
	if got := EnsureRequestID(""); got == "" {
		t.Fatalf("EnsureRequestID should generate a non-empty ID")
	}
}
