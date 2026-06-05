package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
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

type Service struct {
	logger             *slog.Logger
	store              *store.Store
	applyClientFactory clientFactory
}

func NewService(logger *slog.Logger, stores *store.Store) *Service {
	return &Service{
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

func (s *Service) ApplyService(ctx context.Context, planeID string, input ApplyServiceInput) (ApplyResult, error) {
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

func (s *Service) DeleteService(ctx context.Context, planeID string, input DeleteServiceInput) error {
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

func (s *Service) buildExecutionPlan(ctx context.Context, input ApplyServiceInput, spec cloudplaneapi.ServiceSpec) (cloudplaneapi.ExecutionPlanRequest, error) {
	collector := resourceCollector{}
	if spec.ConfigSetID != "" {
		collector.addConfigSet(spec.ConfigSetID)
	}
	if spec.SecretSetID != "" {
		collector.addSecretSet(spec.SecretSetID)
	}
	if spec.RegistryCredentialID != "" {
		collector.addRegistryCredential(spec.RegistryCredentialID)
	}
	for _, item := range projectedfile.CloneSpecs(spec.ProjectedFiles) {
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			collector.addConfigSet(item.SourceID)
		case projectedfile.SourceKindSecretSet:
			collector.addSecretSet(item.SourceID)
		default:
			return cloudplaneapi.ExecutionPlanRequest{}, projectedfile.ErrSourceKindInvalid
		}
	}

	configs := map[string]map[string]string{}
	for _, id := range collector.configSetIDs {
		local, err := s.store.GetConfigSet(ctx, id)
		if err != nil {
			return cloudplaneapi.ExecutionPlanRequest{}, err
		}
		configs[id] = local.Values
	}
	secrets := map[string]map[string]string{}
	for _, id := range collector.secretSetIDs {
		local, err := s.store.GetSecretSet(ctx, id)
		if err != nil {
			return cloudplaneapi.ExecutionPlanRequest{}, err
		}
		secrets[id] = local.Values
	}
	var imageCredential *cloudplaneapi.ExecutionImageCredential
	for _, id := range collector.registryCredentialIDs {
		local, err := s.store.GetRegistryCredential(ctx, id)
		if err != nil {
			return cloudplaneapi.ExecutionPlanRequest{}, err
		}
		if id == spec.RegistryCredentialID {
			imageCredential = &cloudplaneapi.ExecutionImageCredential{
				Server:   local.Server,
				Username: local.Username,
				Password: local.Password,
			}
		}
	}
	env := cloneStringMap(spec.Env)
	if spec.SecretSetID != "" {
		for key, value := range secrets[spec.SecretSetID] {
			env[key] = value
		}
	}
	projectedFiles, err := materializeProjectedFiles(spec.ProjectedFiles, configs, secrets)
	if err != nil {
		return cloudplaneapi.ExecutionPlanRequest{}, err
	}
	persistentDirs, err := materializePersistentDirs(input.Metadata.ID, spec.PersistentDirs)
	if err != nil {
		return cloudplaneapi.ExecutionPlanRequest{}, err
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
		ProjectedFiles:    projectedFiles,
		PersistentDirs:    persistentDirs,
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

func materializeProjectedFiles(specs []projectedfile.Spec, configs map[string]map[string]string, secrets map[string]map[string]string) ([]cloudplaneapi.ExecutionProjectedFile, error) {
	out := make([]cloudplaneapi.ExecutionProjectedFile, 0, len(specs))
	for _, item := range projectedfile.CloneSpecs(specs) {
		var values map[string]string
		sensitive := false
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			values = configs[item.SourceID]
		case projectedfile.SourceKindSecretSet:
			values = secrets[item.SourceID]
			sensitive = true
		default:
			return nil, projectedfile.ErrSourceKindInvalid
		}
		value, ok := values[item.SourceKey]
		if !ok {
			return nil, fmt.Errorf("projected file source %s does not contain key %s", item.SourceID, item.SourceKey)
		}
		out = append(out, cloudplaneapi.ExecutionProjectedFile{
			MountPath: item.MountPath,
			Content:   value,
			Mode:      projectedfile.DefaultMode(item.SourceKind),
			Sensitive: sensitive,
		})
	}
	return out, nil
}

func materializePersistentDirs(serviceID string, specs []persistentdir.Spec) ([]cloudplaneapi.ExecutionPersistentDir, error) {
	mounts, err := persistentdir.MaterializeMounts("", serviceID, specs)
	if err != nil {
		return nil, err
	}
	out := make([]cloudplaneapi.ExecutionPersistentDir, 0, len(mounts))
	for _, item := range mounts {
		out = append(out, cloudplaneapi.ExecutionPersistentDir{
			Name:       item.Name,
			MountPath:  item.MountPath,
			SourcePath: item.SourcePath,
		})
	}
	return out, nil
}

type resourceCollector struct {
	configSetIDs           []string
	secretSetIDs           []string
	registryCredentialIDs  []string
	seenConfigSet          map[string]struct{}
	seenSecretSet          map[string]struct{}
	seenRegistryCredential map[string]struct{}
}

func (c *resourceCollector) addConfigSet(id string) {
	if id == "" {
		return
	}
	if c.seenConfigSet == nil {
		c.seenConfigSet = make(map[string]struct{})
	}
	if _, ok := c.seenConfigSet[id]; ok {
		return
	}
	c.seenConfigSet[id] = struct{}{}
	c.configSetIDs = append(c.configSetIDs, id)
}

func (c *resourceCollector) addSecretSet(id string) {
	if id == "" {
		return
	}
	if c.seenSecretSet == nil {
		c.seenSecretSet = make(map[string]struct{})
	}
	if _, ok := c.seenSecretSet[id]; ok {
		return
	}
	c.seenSecretSet[id] = struct{}{}
	c.secretSetIDs = append(c.secretSetIDs, id)
}

func (c *resourceCollector) addRegistryCredential(id string) {
	if id == "" {
		return
	}
	if c.seenRegistryCredential == nil {
		c.seenRegistryCredential = make(map[string]struct{})
	}
	if _, ok := c.seenRegistryCredential[id]; ok {
		return
	}
	c.seenRegistryCredential[id] = struct{}{}
	c.registryCredentialIDs = append(c.registryCredentialIDs, id)
}
