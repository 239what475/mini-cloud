package serviceops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/contract/cloudplaneapi"
	"mini-cloud/internal/controlplane/model"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

var (
	ErrPlaneIDRequired    = errors.New("planeID is required")
	ErrServiceIDRequired  = errors.New("serviceID is required")
	ErrRegionRequired     = errors.New("region is required")
	ErrPlaneNotRegistered = errors.New("plane southbound registration must complete before service apply actions can run")
)

const (
	defaultApplyServiceTimeout  = 20 * time.Minute
	defaultDeleteServiceTimeout = 2 * time.Minute
)

type ApplyResult struct {
	PlaneID string `json:"planeID"`
	Action  string `json:"action"`
	PlanID  string `json:"planID"`
}

type DeleteServiceInput struct {
	ServiceID         string `json:"serviceID"`
	ServiceGeneration int64  `json:"serviceGeneration"`
	PlanID            string `json:"planID"`
}

type Dispatcher struct {
	logger *slog.Logger
	store  *store.Store
}

func NewDispatcher(logger *slog.Logger, stores *store.Store) *Dispatcher {
	return &Dispatcher{
		logger: logger,
		store:  stores,
	}
}

func (s *Dispatcher) ApplyService(ctx context.Context, planeID string, service model.Service) (ApplyResult, error) {
	if s == nil || s.store == nil {
		return ApplyResult{}, fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return ApplyResult{}, ErrPlaneIDRequired
	}

	plane, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return ApplyResult{}, err
	}
	if !plane.Registration.Registered {
		return ApplyResult{}, ErrPlaneNotRegistered
	}
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return ApplyResult{}, err
	}
	client, err := planeclient.New(plane.GRPCEndpoint, token)
	if err != nil {
		return ApplyResult{}, err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultApplyServiceTimeout)
	defer cancel()

	plan, err := executionPlanRequest(service, plane.Region)
	if err != nil {
		return ApplyResult{}, err
	}
	accepted, err := client.ApplyExecutionPlan(requestCtx, plan)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply execution plan: %w", err)
	}

	return ApplyResult{
		PlaneID: planeID,
		Action:  accepted.Action,
		PlanID:  accepted.PlanID,
	}, nil
}

func (s *Dispatcher) DeleteService(ctx context.Context, planeID string, input DeleteServiceInput) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return ErrPlaneIDRequired
	}
	if input.ServiceID == "" {
		return ErrServiceIDRequired
	}
	if input.ServiceGeneration <= 0 {
		return fmt.Errorf("serviceGeneration must be greater than 0")
	}
	if strings.TrimSpace(input.PlanID) == "" {
		return fmt.Errorf("planID is required")
	}

	plane, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return err
	}
	if !plane.Registration.Registered {
		return ErrPlaneNotRegistered
	}

	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return err
	}
	client, err := planeclient.New(plane.GRPCEndpoint, token)
	if err != nil {
		return err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultDeleteServiceTimeout)
	defer cancel()

	if err := client.DeleteExecutionPlan(requestCtx, cloudplaneapi.DeleteExecutionPlanRequest{
		ServiceID:         input.ServiceID,
		ServiceGeneration: input.ServiceGeneration,
		PlanID:            input.PlanID,
	}); err != nil {
		return fmt.Errorf("delete execution plan: %w", err)
	}
	return nil
}

func executionPlanRequest(service model.Service, defaultRegion string) (cloudplaneapi.ExecutionPlanRequest, error) {
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return cloudplaneapi.ExecutionPlanRequest{}, ErrServiceIDRequired
	}
	if strings.TrimSpace(defaultRegion) == "" {
		return cloudplaneapi.ExecutionPlanRequest{}, ErrRegionRequired
	}

	env := cloneEnvMap(service.Spec.Env)
	for key, value := range service.Spec.SecretEnv {
		env[key] = value
	}
	return cloudplaneapi.ExecutionPlanRequest{
		PlanID:            fmt.Sprintf("%s-g%d", service.Metadata.ID, service.Metadata.Generation),
		ServiceID:         service.Metadata.ID,
		ServiceName:       service.Metadata.Name,
		ServiceGeneration: service.Metadata.Generation,
		Image:             service.Spec.Image,
		Command:           append([]string(nil), service.Spec.Command...),
		Args:              append([]string(nil), service.Spec.Args...),
		Env:               env,
		ProjectedFiles:    executionProjectedFiles(service.Spec.Files),
		ImageCredential:   executionImageCredential(service.Spec.RegistryCredential),
		ContainerPort:     service.Spec.DefaultPort,
		ReadinessPath:     service.Spec.ReadinessPath,
		InstanceClass:     service.Spec.InstanceClass,
		Exposure:          service.Spec.Exposure,
	}, nil
}

func executionImageCredential(input *model.ServiceRegistryCredential) *cloudplaneapi.ExecutionImageCredential {
	if input == nil {
		return nil
	}
	return &cloudplaneapi.ExecutionImageCredential{
		Server:   input.Server,
		Username: input.Username,
		Password: input.Password,
	}
}

func cloneEnvMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func executionProjectedFiles(files []projectedfile.File) []cloudplaneapi.ExecutionProjectedFile {
	out := make([]cloudplaneapi.ExecutionProjectedFile, 0, len(files))
	for _, item := range projectedfile.CloneFiles(files) {
		out = append(out, cloudplaneapi.ExecutionProjectedFile{
			MountPath: item.MountPath,
			Content:   item.Content,
			Mode:      item.Mode,
			Sensitive: item.Sensitive,
		})
	}
	return out
}
