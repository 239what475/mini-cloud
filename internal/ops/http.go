package ops

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func getHTTPStatus(ctx context.Context, url string, timeout time.Duration) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

func waitForHTTPStatus(ctx context.Context, url string, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastErr error
	for {
		status, err := getHTTPStatus(ctx, url, 20*time.Second)
		if err != nil {
			lastErr = err
		} else {
			lastStatus = status
			if status == want {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("last status %d, want %d", lastStatus, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
