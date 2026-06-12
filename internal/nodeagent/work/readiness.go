package work

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultAttempts = 1
	defaultInterval = time.Second
	defaultTimeout  = time.Second
)

type ReadinessConfig struct {
	URL      string
	Attempts int
	Interval time.Duration
	Timeout  time.Duration
}

type ReadinessObservation struct {
	Attempt    int
	StartedAt  time.Time
	StatusCode int
	Error      string
}

type ReadinessResult struct {
	URL          string
	Passed       bool
	PassedAt     *time.Time
	Observations []ReadinessObservation
}

func WaitReadiness(ctx context.Context, cfg ReadinessConfig) ReadinessResult {
	if cfg.Attempts <= 0 {
		cfg.Attempts = defaultAttempts
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}

	result := ReadinessResult{
		URL:          cfg.URL,
		Observations: make([]ReadinessObservation, 0, cfg.Attempts),
	}
	client := &http.Client{Timeout: cfg.Timeout}

	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		observation := probeReadiness(ctx, client, cfg, attempt)
		result.Observations = append(result.Observations, observation)
		if observation.Error == "" && observation.StatusCode >= 200 && observation.StatusCode < 400 {
			passedAt := observation.StartedAt
			result.Passed = true
			result.PassedAt = &passedAt
			return result
		}
		if ctx.Err() != nil || attempt == cfg.Attempts {
			return result
		}
		if !sleep(ctx, cfg.Interval) {
			return result
		}
	}

	return result
}

func probeReadiness(ctx context.Context, client *http.Client, cfg ReadinessConfig, attempt int) ReadinessObservation {
	observation := ReadinessObservation{
		Attempt:   attempt,
		StartedAt: time.Now().UTC(),
	}
	if err := ctx.Err(); err != nil {
		observation.Error = err.Error()
		return observation
	}

	probeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		observation.Error = err.Error()
		return observation
	}

	resp, err := client.Do(req)
	if err != nil {
		observation.Error = err.Error()
		return observation
	}
	observation.StatusCode = resp.StatusCode
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		observation.Error = fmt.Sprintf("drain response body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil && observation.Error == "" {
		observation.Error = fmt.Sprintf("close response body: %v", closeErr)
		return observation
	}
	if observation.Error != "" {
		return observation
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		observation.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return observation
}

func sleep(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
