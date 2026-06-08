package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/contract/cloudplaneapi"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

var ErrPlaneNotRegistered = fmt.Errorf("plane southbound registration must complete before service apply actions can run")
var ErrPlaneNotAcceptingNewRuns = fmt.Errorf("plane is not accepting new runs")

type clientFactory func(grpcEndpoint string, bearerToken string) (*planeclient.Client, error)

const (
	defaultApplyServiceTimeout  = 20 * time.Minute
	defaultDeleteServiceTimeout = 2 * time.Minute
)

type Dispatcher struct {
	logger             *slog.Logger
	store              *store.Store
	applyClientFactory clientFactory
}

func NewDispatcher(logger *slog.Logger, stores *store.Store) *Dispatcher {
	return &Dispatcher{
		logger:             logger,
		store:              stores,
		applyClientFactory: newClientFactory(),
	}
}

func newClientFactory() clientFactory {
	return func(grpcEndpoint string, bearerToken string) (*planeclient.Client, error) {
		return planeclient.New(grpcEndpoint, bearerToken)
	}
}

func (s *Dispatcher) ApplyService(ctx context.Context, planeID string, input ApplyServiceInput) (ApplyResult, error) {
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
	if !plane.Operation.AcceptingNewRuns() {
		return ApplyResult{}, fmt.Errorf("%w: plane operation state is %s", ErrPlaneNotAcceptingNewRuns, plane.Operation.ResolvedState())
	}
	spec, err := input.ResolvedSpec(plane.Region)
	if err != nil {
		return ApplyResult{}, err
	}
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return ApplyResult{}, err
	}
	client, err := s.applyClientFactory(plane.GRPCEndpoint, token)
	if err != nil {
		return ApplyResult{}, err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultApplyServiceTimeout)
	defer cancel()

	plan, err := s.buildExecutionPlan(requestCtx, input, spec)
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
	client, err := s.applyClientFactory(plane.GRPCEndpoint, token)
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

func (s *Dispatcher) buildExecutionPlan(ctx context.Context, input ApplyServiceInput, spec cloudplaneapi.ServiceSpec) (cloudplaneapi.ExecutionPlanRequest, error) {
	var imageCredential *cloudplaneapi.ExecutionImageCredential
	if spec.RegistryCredentialID != "" {
		local, err := s.store.GetRegistryCredential(ctx, spec.RegistryCredentialID)
		if err != nil {
			return cloudplaneapi.ExecutionPlanRequest{}, err
		}
		imageCredential = &cloudplaneapi.ExecutionImageCredential{
			Server:   local.Server,
			Username: local.Username,
			Password: local.Password,
		}
	}
	env := cloneStringMap(spec.Env)
	for key, value := range spec.SecretEnv {
		env[key] = value
	}
	return cloudplaneapi.ExecutionPlanRequest{
		PlanID:            fmt.Sprintf("%s-g%d", input.Metadata.ID, input.Metadata.Generation),
		ServiceID:         input.Metadata.ID,
		ServiceName:       input.Metadata.Name,
		ServiceGeneration: input.Metadata.Generation,
		Image:             spec.Image,
		Command:           append([]string(nil), spec.Command...),
		Args:              append([]string(nil), spec.Args...),
		Env:               env,
		ProjectedFiles:    executionProjectedFiles(spec.Files),
		ImageCredential:   imageCredential,
		ContainerPort:     spec.DefaultPort,
		ReadinessPath:     spec.ReadinessPath,
		InstanceClass:     spec.InstanceClass,
		Exposure:          spec.Exposure,
	}, nil
}

func cloneStringMap(input map[string]string) map[string]string {
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
