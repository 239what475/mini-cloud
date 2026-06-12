package cloudplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/transport"
)

const planeRegistrationRetryInterval = 10 * time.Second

type registrationRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	GRPCEndpoint string `json:"grpcEndpoint"`
}

func registerWithControlPlaneUntilReady(ctx context.Context, logger *slog.Logger, cfg cloudplaneconfig.Config) {
	runControlPlaneRegistrationLoop(ctx, logger, cfg, planeRegistrationRetryInterval)
}

func runControlPlaneRegistrationLoop(ctx context.Context, logger *slog.Logger, cfg cloudplaneconfig.Config, retryInterval time.Duration) {
	if logger == nil {
		logger = slog.Default()
	}
	if retryInterval <= 0 {
		retryInterval = planeRegistrationRetryInterval
	}
	for {
		if err := registerWithControlPlane(ctx, cfg); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("register cloud-plane with control-plane failed; will retry", "error", err)
		} else {
			logger.Info("cloud-plane registered with control-plane")
			return
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func registerWithControlPlane(ctx context.Context, cfg cloudplaneconfig.Config) error {
	body, err := json.Marshal(registrationRequest{
		Name:         cfg.Plane.Name,
		DisplayName:  cfg.Plane.Name,
		Provider:     cfg.Infrastructure.Provider,
		Region:       cfg.Infrastructure.RegionID,
		GRPCEndpoint: cfg.Plane.GRPCEndpoint,
	})
	if err != nil {
		return fmt.Errorf("marshal plane registration: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, cfg.ControlPlane.URL+"/api/v1/internal/planes/register", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build plane registration request: %w", err)
	}
	request.Header.Set("Authorization", transport.BearerHeader(cfg.ControlPlane.BearerToken))
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("register plane with control-plane: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("register plane with control-plane returned %s", response.Status)
	}
	return nil
}
