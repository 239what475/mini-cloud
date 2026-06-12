package work

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitEventuallyPasses(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := attempts.Add(1)
		if current == 1 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
	}))
	t.Cleanup(server.Close)

	result := WaitReadiness(context.Background(), ReadinessConfig{
		URL:      server.URL + "/healthz",
		Attempts: 3,
		Interval: 5 * time.Millisecond,
		Timeout:  time.Second,
	})

	if !result.Passed {
		t.Fatalf("expected readiness check to pass, got %+v", result)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(result.Observations))
	}
	if result.Observations[0].StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected first observation status 503, got %+v", result.Observations[0])
	}
	if result.Observations[1].StatusCode != http.StatusOK {
		t.Fatalf("expected second observation status 200, got %+v", result.Observations[1])
	}
	if result.PassedAt == nil {
		t.Fatalf("expected PassedAt to be set")
	}
}

func TestWaitReturnsFailureAfterAllAttempts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	result := WaitReadiness(context.Background(), ReadinessConfig{
		URL:      server.URL + "/healthz",
		Attempts: 2,
		Interval: 5 * time.Millisecond,
		Timeout:  time.Second,
	})

	if result.Passed {
		t.Fatalf("expected readiness check to fail, got %+v", result)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(result.Observations))
	}
	if result.Observations[1].Error != "unexpected status 502" {
		t.Fatalf("expected last error to describe status 502, got %+v", result.Observations[1])
	}
}

func TestWaitNormalizesInvalidRetryConfig(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attempts.Add(1)
	}))
	t.Cleanup(server.Close)

	result := WaitReadiness(context.Background(), ReadinessConfig{
		URL:      server.URL + "/healthz",
		Attempts: -1,
		Interval: -1,
		Timeout:  -1,
	})

	if !result.Passed {
		t.Fatalf("expected readiness check to pass, got %+v", result)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}
