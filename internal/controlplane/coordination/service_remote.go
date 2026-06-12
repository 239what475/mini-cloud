package coordination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

var (
	errPlaneIDRequired   = errors.New("planeID is required")
	errServiceIDRequired = errors.New("serviceID is required")
)

const (
	dispatchApplyTimeout  = 15 * time.Second
	dispatchDeleteTimeout = 15 * time.Second
)

func (c *ServiceOperations) dispatchRemoteService(ctx context.Context, planeID string, service model.Service) error {
	if planeID == "" {
		return errPlaneIDRequired
	}

	client, err := c.planeClient(ctx, planeID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	request, err := applyServiceRequest(service)
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, dispatchApplyTimeout)
	defer cancel()

	if err := client.ApplyService(requestCtx, request); err != nil {
		return fmt.Errorf("apply service to cloud-plane: %w", err)
	}
	return nil
}

func (c *ServiceOperations) deleteRemoteService(ctx context.Context, planeID string, serviceID string, serviceGeneration int64) error {
	if planeID == "" {
		return errPlaneIDRequired
	}
	if serviceID == "" {
		return errServiceIDRequired
	}
	if serviceGeneration <= 0 {
		return fmt.Errorf("serviceGeneration must be greater than 0")
	}

	client, err := c.planeClient(ctx, planeID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	requestCtx, cancel := context.WithTimeout(ctx, dispatchDeleteTimeout)
	defer cancel()

	if err := client.DeleteService(requestCtx, &cloudplanev1.DeleteServiceRequest{
		ServiceId:         serviceID,
		ServiceGeneration: serviceGeneration,
	}); err != nil {
		return fmt.Errorf("delete service from cloud-plane: %w", err)
	}
	return nil
}

func (c *ServiceOperations) planeClient(ctx context.Context, planeID string) (*planeClient, error) {
	plane, err := c.store.GetPlane(ctx, planeID)
	if err != nil {
		return nil, err
	}
	return newPlaneClient(plane.GRPCEndpoint, c.southboundToken)
}

func applyServiceRequest(service model.Service) (*cloudplanev1.ApplyServiceRequest, error) {
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return nil, errServiceIDRequired
	}

	env := make(map[string]string, len(service.Spec.Env))
	for key, value := range service.Spec.Env {
		env[key] = value
	}
	return &cloudplanev1.ApplyServiceRequest{
		ServiceId:         service.Metadata.ID,
		ServiceName:       service.Metadata.Name,
		DisplayName:       service.Metadata.DisplayName,
		Host:              service.Metadata.Host,
		ServiceGeneration: service.Metadata.Generation,
		Image:             service.Spec.Image,
		Command:           append([]string(nil), service.Spec.Command...),
		Args:              append([]string(nil), service.Spec.Args...),
		Env:               env,
		ContainerPort:     int32(service.Spec.DefaultPort),
		ReadinessPath:     service.Spec.ReadinessPath,
		InstanceClass:     service.Spec.InstanceClass,
		Exposure:          service.Spec.Exposure,
	}, nil
}
