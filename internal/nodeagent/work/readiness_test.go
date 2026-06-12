package work

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitEventuallyPasses(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	checker := NewReadinessChecker(roundTripFunc(func(*http.Request) (*http.Response, error) {
		current := attempts.Add(1)
		if current == 1 {
			return newHTTPResponse(http.StatusServiceUnavailable), nil
		}
		return newHTTPResponse(http.StatusOK), nil
	}))
	result := checker.Wait(context.Background(), ReadinessConfig{
		URL:      "http://service.local/healthz",
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

	checker := NewReadinessChecker(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return newHTTPResponse(http.StatusBadGateway), nil
	}))
	result := checker.Wait(context.Background(), ReadinessConfig{
		URL:      "http://service.local/healthz",
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
	checker := NewReadinessChecker(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return newHTTPResponse(http.StatusOK), nil
	}))
	result := checker.Wait(context.Background(), ReadinessConfig{
		URL:      "http://service.local/healthz",
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}
func newHTTPResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}
}
