package cloudplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/transport"
)

type registrationRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	GRPCEndpoint string `json:"grpcEndpoint"`
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
