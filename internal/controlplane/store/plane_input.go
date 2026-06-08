package store

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
)

var (
	errPlaneNameRequired            = errors.New("name is required")
	errInvalidPlaneName             = errors.New("name must use lowercase letters, digits, and hyphens")
	errPlaneDisplayNameRequired     = errors.New("displayName is required")
	errPlaneProviderRequired        = errors.New("provider is required")
	errPlaneRegionRequired          = errors.New("region is required")
	errPlaneGRPCEndpointRequired    = errors.New("grpcEndpoint is required")
	errPlaneSouthboundTokenRequired = errors.New("southboundToken is required")
	errInvalidPlaneGRPCEndpoint     = errors.New("grpcEndpoint must be a gRPC target such as host:port, grpc://host:port, grpcs://host:port, dns:///name:port, or unix:///path")
	errInvalidPlaneStatus           = errors.New("status must be one of registering, ready, degraded, offline")
	planeNamePattern                = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type CreatePlaneInput struct {
	Name            string `json:"name"`
	DisplayName     string `json:"displayName"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	GRPCEndpoint    string `json:"grpcEndpoint"`
	SouthboundToken string `json:"southboundToken"`
}

type UpdatePlaneStatusInput struct {
	Status          string     `json:"status"`
	Message         string     `json:"message"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
}

func (in CreatePlaneInput) validate() error {
	switch {
	case strings.TrimSpace(in.Name) == "":
		return invalidInput(errPlaneNameRequired)
	case !planeNamePattern.MatchString(strings.TrimSpace(in.Name)):
		return invalidInput(errInvalidPlaneName)
	case strings.TrimSpace(in.DisplayName) == "":
		return invalidInput(errPlaneDisplayNameRequired)
	case strings.TrimSpace(in.Provider) == "":
		return invalidInput(errPlaneProviderRequired)
	case strings.TrimSpace(in.Region) == "":
		return invalidInput(errPlaneRegionRequired)
	case strings.TrimSpace(in.SouthboundToken) == "":
		return invalidInput(errPlaneSouthboundTokenRequired)
	}
	_, err := normalizeGRPCEndpoint(in.GRPCEndpoint)
	return invalidInput(err)
}

func (in UpdatePlaneStatusInput) validate() error {
	if !model.IsStatus(in.Status) {
		return invalidInput(errInvalidPlaneStatus)
	}
	return nil
}

func normalizeGRPCEndpoint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errPlaneGRPCEndpointRequired
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", errInvalidPlaneGRPCEndpoint
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", errInvalidPlaneGRPCEndpoint
	}
	if strings.HasPrefix(lower, "grpc://") {
		target := strings.TrimSpace(value[len("grpc://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", errInvalidPlaneGRPCEndpoint
		}
		return target, nil
	}
	if strings.HasPrefix(lower, "grpcs://") || strings.HasPrefix(lower, "dns:///") || strings.HasPrefix(lower, "unix:///") {
		return value, nil
	}
	return value, nil
}
