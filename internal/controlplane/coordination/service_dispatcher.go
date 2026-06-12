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
	dispatchApplyTimeout  = 15 * time.Second
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

func (s *serviceDispatcher) ApplyService(ctx context.Context, planeID string, service model.Service) (model.Service, error) {
	if planeID == "" {
		return model.Service{}, errPlaneIDRequired
	}

	client, err := s.planeClient(ctx, planeID)
	if err != nil {
		return model.Service{}, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	request, err := applyServiceRequest(service)
	if err != nil {
		return model.Service{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, dispatchApplyTimeout)
	defer cancel()

	applied, err := client.ApplyService(requestCtx, request)
	if err != nil {
		return model.Service{}, fmt.Errorf("apply service to cloud-plane: %w", err)
	}
	return serviceFromProto(planeID, applied.GetService()), nil
}

func (s *serviceDispatcher) DeleteService(ctx context.Context, planeID string, serviceID string, serviceGeneration int64) error {
	if planeID == "" {
		return errPlaneIDRequired
	}
	if serviceID == "" {
		return errServiceIDRequired
	}
	if serviceGeneration <= 0 {
		return fmt.Errorf("serviceGeneration must be greater than 0")
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

	if err := client.DeleteService(requestCtx, &cloudplanev1.DeleteServiceRequest{
		ServiceId:         serviceID,
		ServiceGeneration: serviceGeneration,
	}); err != nil {
		return fmt.Errorf("delete service from cloud-plane: %w", err)
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

func serviceFromProto(planeID string, input *cloudplanev1.PlaneService) model.Service {
	if input == nil {
		return model.Service{}
	}
	spec := input.GetSpec()
	env := make(map[string]string, len(spec.GetEnv()))
	for key, value := range spec.GetEnv() {
		env[key] = value
	}
	phase := model.PhaseProgressing
	if input.GetDesiredState() == model.DesiredStateDeleted {
		phase = model.PhaseDeleting
	}
	return model.Service{
		Metadata: model.ServiceMetadata{
			ID:          input.GetServiceId(),
			Name:        input.GetName(),
			DisplayName: input.GetDisplayName(),
			Host:        input.GetHost(),
			Generation:  input.GetGeneration(),
		},
		Spec: model.ServiceSpec{
			PlaneID:       planeID,
			InstanceClass: spec.GetInstanceClass(),
			Exposure:      spec.GetExposure(),
			Image:         spec.GetImage(),
			Command:       append([]string(nil), spec.GetCommand()...),
			Args:          append([]string(nil), spec.GetArgs()...),
			DefaultPort:   int(spec.GetContainerPort()),
			ReadinessPath: spec.GetReadinessPath(),
			Env:           env,
		},
		Status: model.ServiceStatus{
			DesiredState: input.GetDesiredState(),
			Observed: model.ServiceObservedStatus{
				ObservedGeneration: input.GetGeneration(),
				Phase:              phase,
				Message:            "service accepted by cloud-plane",
			},
			Run: model.PendingRunStatus("waiting for cloud-plane execution"),
		},
	}
}
