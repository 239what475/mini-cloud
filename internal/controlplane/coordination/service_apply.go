package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

var (
	errPlaneIDRequired         = errors.New("planeID is required")
	errServiceIDRequired       = errors.New("serviceID is required")
	errRegionRequired          = errors.New("region is required")
	errPlaneApplyNotRegistered = errors.New("plane southbound registration must complete before service apply actions can run")
)

const (
	defaultApplyServiceTimeout  = 20 * time.Minute
	defaultDeleteServiceTimeout = 2 * time.Minute
)

type applyResult struct {
	PlaneID string `json:"planeID"`
	Action  string `json:"action"`
	PlanID  string `json:"planID"`
}

type deleteServiceInput struct {
	ServiceID         string `json:"serviceID"`
	ServiceGeneration int64  `json:"serviceGeneration"`
	PlanID            string `json:"planID"`
}

type serviceApplier struct {
	logger *slog.Logger
	store  *store.Store
}

func newServiceApplier(logger *slog.Logger, stores *store.Store) *serviceApplier {
	return &serviceApplier{
		logger: logger,
		store:  stores,
	}
}

func (s *serviceApplier) ApplyService(ctx context.Context, planeID string, service model.Service) (applyResult, error) {
	if s == nil || s.store == nil {
		return applyResult{}, fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return applyResult{}, errPlaneIDRequired
	}

	plane, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return applyResult{}, err
	}
	if !plane.Registration.Registered {
		return applyResult{}, errPlaneApplyNotRegistered
	}
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return applyResult{}, err
	}
	client, err := newPlaneClient(plane.GRPCEndpoint, token)
	if err != nil {
		return applyResult{}, err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultApplyServiceTimeout)
	defer cancel()

	plan, err := executionPlanRequest(service, plane.Region)
	if err != nil {
		return applyResult{}, err
	}
	accepted, err := client.ApplyExecutionPlan(requestCtx, plan)
	if err != nil {
		return applyResult{}, fmt.Errorf("apply execution plan: %w", err)
	}

	return applyResult{
		PlaneID: planeID,
		Action:  accepted.GetAction(),
		PlanID:  accepted.GetPlanId(),
	}, nil
}

func (s *serviceApplier) DeleteService(ctx context.Context, planeID string, input deleteServiceInput) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return errPlaneIDRequired
	}
	if input.ServiceID == "" {
		return errServiceIDRequired
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
		return errPlaneApplyNotRegistered
	}

	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return err
	}
	client, err := newPlaneClient(plane.GRPCEndpoint, token)
	if err != nil {
		return err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultDeleteServiceTimeout)
	defer cancel()

	if err := client.DeleteExecutionPlan(requestCtx, &cloudplanev1.DeleteExecutionPlanRequest{
		ServiceId:         input.ServiceID,
		ServiceGeneration: input.ServiceGeneration,
		PlanId:            input.PlanID,
	}); err != nil {
		return fmt.Errorf("delete execution plan: %w", err)
	}
	return nil
}

func executionPlanRequest(service model.Service, defaultRegion string) (*cloudplanev1.ApplyExecutionPlanRequest, error) {
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return nil, errServiceIDRequired
	}
	if strings.TrimSpace(defaultRegion) == "" {
		return nil, errRegionRequired
	}

	env := cloneEnvMap(service.Spec.Env)
	for key, value := range service.Spec.SecretEnv {
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
		ProjectedFiles:    executionProjectedFiles(service.Spec.Files),
		ImageCredential:   executionImageCredential(service.Spec.RegistryCredential),
		ContainerPort:     int32(service.Spec.DefaultPort),
		ReadinessPath:     service.Spec.ReadinessPath,
		InstanceClass:     service.Spec.InstanceClass,
		Exposure:          service.Spec.Exposure,
	}, nil
}

func executionImageCredential(input *model.ServiceRegistryCredential) *cloudplanev1.ExecutionImageCredential {
	if input == nil {
		return nil
	}
	return &cloudplanev1.ExecutionImageCredential{
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

func executionProjectedFiles(files []projectedfile.File) []*cloudplanev1.ExecutionProjectedFile {
	out := make([]*cloudplanev1.ExecutionProjectedFile, 0, len(files))
	for _, item := range projectedfile.CloneFiles(files) {
		out = append(out, &cloudplanev1.ExecutionProjectedFile{
			MountPath: item.MountPath,
			Content:   item.Content,
			Mode:      item.Mode,
			Sensitive: item.Sensitive,
		})
	}
	return out
}
