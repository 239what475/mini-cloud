package coordination

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/model"
)

var (
	ErrPlaneNotFound   = errors.New("plane not found")
	ErrServiceNotFound = errors.New("service not found")
	ErrInvalidInput    = errors.New("invalid input")
)

type PlaneCatalog struct {
	planes []model.PlaneDetail
	byID   map[string]model.PlaneDetail
}

func NewPlaneCatalog(configured []config.PlaneConfig) (*PlaneCatalog, error) {
	if len(configured) == 0 {
		return nil, fmt.Errorf("%w: planes is required", ErrInvalidInput)
	}
	catalog := &PlaneCatalog{
		planes: make([]model.PlaneDetail, 0, len(configured)),
		byID:   make(map[string]model.PlaneDetail, len(configured)),
	}
	for _, item := range configured {
		plane := model.PlaneDetail{
			Plane: model.Plane{
				ID:           strings.TrimSpace(item.ID),
				Name:         strings.TrimSpace(item.Name),
				DisplayName:  strings.TrimSpace(item.DisplayName),
				Provider:     strings.ToLower(strings.TrimSpace(item.Provider)),
				Region:       strings.TrimSpace(item.Region),
				GRPCEndpoint: strings.TrimSpace(item.GRPCEndpoint),
			},
			Status: model.PlaneStatus{
				PlaneID: strings.TrimSpace(item.ID),
				Status:  model.StatusSyncing,
				Message: "configured; waiting for cloud-plane snapshot",
			},
		}
		if plane.DisplayName == "" {
			plane.DisplayName = plane.Name
		}
		if plane.ID == "" || plane.Name == "" || plane.Provider == "" || plane.Region == "" || plane.GRPCEndpoint == "" {
			return nil, fmt.Errorf("%w: plane requires id, name, provider, region and grpcEndpoint", ErrInvalidInput)
		}
		if _, exists := catalog.byID[plane.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate plane id %q", ErrInvalidInput, plane.ID)
		}
		catalog.byID[plane.ID] = plane
		catalog.planes = append(catalog.planes, plane)
	}
	return catalog, nil
}

func (c *PlaneCatalog) ListPlanes(_ context.Context) ([]model.PlaneDetail, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: plane catalog is nil", ErrInvalidInput)
	}
	out := make([]model.PlaneDetail, len(c.planes))
	copy(out, c.planes)
	return out, nil
}

func (c *PlaneCatalog) GetPlane(_ context.Context, planeID string) (model.PlaneDetail, error) {
	if c == nil {
		return model.PlaneDetail{}, fmt.Errorf("%w: plane catalog is nil", ErrInvalidInput)
	}
	planeID = strings.TrimSpace(planeID)
	if planeID == "" {
		return model.PlaneDetail{}, fmt.Errorf("%w: planeID is required", ErrInvalidInput)
	}
	item, ok := c.byID[planeID]
	if !ok {
		return model.PlaneDetail{}, ErrPlaneNotFound
	}
	return item, nil
}

func newPublicID(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", fmt.Errorf("id prefix is required")
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}
