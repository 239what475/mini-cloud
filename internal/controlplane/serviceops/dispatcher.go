package serviceops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/contract/cloudplaneapi"
	domain "mini-cloud/internal/controlplane/domain"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

var (
	ErrPlaneIDRequired       = errors.New("planeID is required")
	ErrServiceIDRequired     = errors.New("serviceID is required")
	ErrServiceNameRequired   = errors.New("name is required")
	ErrInvalidServiceName    = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired   = errors.New("displayName is required")
	ErrRegionRequired        = errors.New("region is required")
	ErrInvalidInstanceClass  = errors.New("instanceClass must be one of small, medium, large")
	ErrInvalidExposure       = errors.New("exposure must be one of public, private")
	ErrImageRequired         = errors.New("image is required")
	ErrInvalidDefaultPort    = errors.New("defaultPort must be between 1 and 65535")
	ErrInvalidReadinessPath  = errors.New("readinessPath must start with /")
	ErrInvalidEnvironmentKey = errors.New("env keys must not be empty")
	ErrPlaneNotRegistered    = errors.New("plane southbound registration must complete before service apply actions can run")
	serviceNamePattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const (
	defaultApplyServiceTimeout  = 20 * time.Minute
	defaultDeleteServiceTimeout = 2 * time.Minute
)

type ApplyServiceInput struct {
	Metadata ServiceMetadata `json:"metadata"`
	Spec     ServiceSpec     `json:"spec"`
}

type ServiceMetadata struct {
	ID          string `json:"serviceID"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type ServiceSpec struct {
	Region               string               `json:"region"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command"`
	Args                 []string             `json:"args"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env"`
	SecretEnv            map[string]string    `json:"secretEnv,omitempty"`
	RegistryCredentialID string               `json:"registryCredentialID"`
	Files                []projectedfile.File `json:"files,omitempty"`
}

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

func (in ApplyServiceInput) ResolvedSpec(defaultRegion string) (cloudplaneapi.ServiceSpec, error) {
	resolvedRegion := strings.TrimSpace(in.Spec.Region)
	if resolvedRegion == "" {
		resolvedRegion = strings.TrimSpace(defaultRegion)
	}

	if strings.TrimSpace(in.Metadata.ID) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrServiceIDRequired
	}
	if strings.TrimSpace(in.Metadata.Name) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Metadata.Name)) {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidServiceName
	}
	if strings.TrimSpace(in.Metadata.DisplayName) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrDisplayNameRequired
	}
	if strings.TrimSpace(resolvedRegion) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrRegionRequired
	}
	if !domain.IsInstanceClass(in.Spec.InstanceClass) {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidInstanceClass
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(in.Spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidExposure
	}
	if strings.TrimSpace(in.Spec.Image) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrImageRequired
	}
	if in.Spec.DefaultPort <= 0 || in.Spec.DefaultPort > 65535 {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidDefaultPort
	}
	if !strings.HasPrefix(strings.TrimSpace(in.Spec.ReadinessPath), "/") {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidReadinessPath
	}
	for key := range in.Spec.Env {
		if strings.TrimSpace(key) == "" {
			return cloudplaneapi.ServiceSpec{}, ErrInvalidEnvironmentKey
		}
	}
	for key := range in.Spec.SecretEnv {
		if strings.TrimSpace(key) == "" {
			return cloudplaneapi.ServiceSpec{}, ErrInvalidEnvironmentKey
		}
	}
	if err := projectedfile.ValidateFiles(in.Spec.Files); err != nil {
		return cloudplaneapi.ServiceSpec{}, err
	}
	return cloudplaneapi.ServiceSpec{
		Region:               resolvedRegion,
		InstanceClass:        in.Spec.InstanceClass,
		Exposure:             resolvedExposure,
		Image:                in.Spec.Image,
		Command:              append([]string(nil), in.Spec.Command...),
		Args:                 append([]string(nil), in.Spec.Args...),
		DefaultPort:          in.Spec.DefaultPort,
		ReadinessPath:        strings.TrimSpace(in.Spec.ReadinessPath),
		Env:                  in.Spec.Env,
		SecretEnv:            in.Spec.SecretEnv,
		RegistryCredentialID: in.Spec.RegistryCredentialID,
		Files:                projectedfile.CloneFiles(in.Spec.Files),
	}, nil
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
	spec, err := input.ResolvedSpec(plane.Region)
	if err != nil {
		return ApplyResult{}, err
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
	env := cloneEnvMap(spec.Env)
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
