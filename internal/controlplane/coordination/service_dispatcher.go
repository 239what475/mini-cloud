package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

var (
	errPlaneIDRequired   = errors.New("planeID is required")
	errServiceIDRequired = errors.New("serviceID is required")
)

const (
	dispatchRunTimeout    = 15 * time.Second
	dispatchDeleteTimeout = 15 * time.Second
)

type serviceDispatcher struct {
	logger          *slog.Logger
	store           *store.Store
	southboundToken string
}

func newServiceDispatcher(logger *slog.Logger, stores *store.Store, southboundToken string) *serviceDispatcher {
	return &serviceDispatcher{
		logger:          logger,
		store:           stores,
		southboundToken: strings.TrimSpace(southboundToken),
	}
}

func (s *serviceDispatcher) DispatchRun(ctx context.Context, planeID string, service model.Service) (string, error) {
	if planeID == "" {
		return "", errPlaneIDRequired
	}

	client, err := s.planeClient(ctx, planeID)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	plan, err := executionPlanRequest(service)
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, dispatchRunTimeout)
	defer cancel()

	accepted, err := client.ApplyExecutionPlan(requestCtx, plan)
	if err != nil {
		return "", fmt.Errorf("dispatch execution plan: %w", err)
	}

	return accepted.GetPlanId(), nil
}

func (s *serviceDispatcher) DispatchDelete(ctx context.Context, planeID string, serviceID string, serviceGeneration int64, planID string) error {
	if planeID == "" {
		return errPlaneIDRequired
	}
	if serviceID == "" {
		return errServiceIDRequired
	}
	if serviceGeneration <= 0 {
		return fmt.Errorf("serviceGeneration must be greater than 0")
	}
	if strings.TrimSpace(planID) == "" {
		return fmt.Errorf("planID is required")
	}

	client, err := s.planeClient(ctx, planeID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	requestCtx, cancel := context.WithTimeout(ctx, dispatchDeleteTimeout)
	defer cancel()

	if err := client.DeleteExecutionPlan(requestCtx, &cloudplanev1.DeleteExecutionPlanRequest{
		ServiceId:         serviceID,
		ServiceGeneration: serviceGeneration,
		PlanId:            planID,
	}); err != nil {
		return fmt.Errorf("delete execution plan: %w", err)
	}
	return nil
}

func (s *serviceDispatcher) planeClient(ctx context.Context, planeID string) (*planeClient, error) {
	plane, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return nil, err
	}
	return newPlaneClient(plane.GRPCEndpoint, s.southboundToken)
}

func executionPlanRequest(service model.Service) (*cloudplanev1.ApplyExecutionPlanRequest, error) {
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return nil, errServiceIDRequired
	}

	env := make(map[string]string, len(service.Spec.Env))
	for key, value := range service.Spec.Env {
		env[key] = value
	}
	return &cloudplanev1.ApplyExecutionPlanRequest{
		PlanId:            fmt.Sprintf("%s-g%d", service.Metadata.ID, service.Metadata.Generation),
		ServiceId:         service.Metadata.ID,
		ServiceName:       service.Metadata.Name,
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
