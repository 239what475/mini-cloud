package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	controlproject "mini-cloud/internal/common/project"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/contract/cloudplaneapi"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

var ErrPlaneNotRegistered = fmt.Errorf("plane southbound registration must complete before service apply actions can run")
var ErrPlaneNotAcceptingNewDeployments = fmt.Errorf("plane is not accepting new deployments")

type clientFactory func(grpcEndpoint string, bearerToken string) (*planeclient.Client, error)

const (
	defaultApplyServiceTimeout  = 20 * time.Minute
	defaultDeleteServiceTimeout = 2 * time.Minute
	defaultReadServiceTimeout   = 2 * time.Minute
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
	if !plane.Operation.AcceptingNewDeployments() {
		return ApplyResult{}, fmt.Errorf("%w: plane operation state is %s", ErrPlaneNotAcceptingNewDeployments, plane.Operation.ResolvedState())
	}
	request, err := input.ResolvedApplyRequest(plane.Region)
	if err != nil {
		return ApplyResult{}, err
	}
	globalProject, err := s.store.GetProject(ctx, input.Metadata.ProjectID)
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

	project, err := s.buildProject(requestCtx, globalProject, &request)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.applyProject(requestCtx, client, project); err != nil {
		return ApplyResult{}, err
	}

	accepted, err := client.ApplyService(requestCtx, input.Metadata.ProjectID, input.Metadata.ID, input.Metadata.Name, request)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply service: %w", err)
	}

	return ApplyResult{
		PlaneID:           planeID,
		ProjectID:         input.Metadata.ProjectID,
		Action:            accepted.Action,
		DesiredGeneration: accepted.DesiredGeneration,
	}, nil
}

func (s *Service) applyProject(ctx context.Context, client *planeclient.Client, project cloudplaneapi.Project) error {
	if _, err := client.ApplyProject(ctx, project); err != nil {
		return fmt.Errorf("apply project to plane: %w", err)
	}
	return nil
}

func (s *Service) DeleteService(ctx context.Context, planeID string, projectID string, serviceID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return ErrPlaneIDRequired
	}
	if projectID == "" {
		return ErrProjectIDRequired
	}
	if serviceID == "" {
		return ErrServiceIDRequired
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

	if err := client.DeleteService(requestCtx, projectID, serviceID); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func (s *Service) GetService(ctx context.Context, planeID string, projectID string, serviceID string) (cloudplaneapi.ServiceResponse, error) {
	if s == nil || s.store == nil {
		return cloudplaneapi.ServiceResponse{}, fmt.Errorf("deploy service is not configured")
	}
	if planeID == "" {
		return cloudplaneapi.ServiceResponse{}, ErrPlaneIDRequired
	}
	if projectID == "" {
		return cloudplaneapi.ServiceResponse{}, ErrProjectIDRequired
	}
	if serviceID == "" {
		return cloudplaneapi.ServiceResponse{}, ErrServiceIDRequired
	}

	plane, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return cloudplaneapi.ServiceResponse{}, err
	}
	if !plane.Registration.Registered {
		return cloudplaneapi.ServiceResponse{}, ErrPlaneNotRegistered
	}

	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		return cloudplaneapi.ServiceResponse{}, err
	}
	client, err := s.applyClientFactory(plane.GRPCEndpoint, token)
	if err != nil {
		return cloudplaneapi.ServiceResponse{}, err
	}
	defer util.CloseAndLog(s.logger, "plane client", client, "plane_id", planeID)

	requestCtx, cancel := context.WithTimeout(ctx, defaultReadServiceTimeout)
	defer cancel()

	response, err := client.GetService(requestCtx, projectID, serviceID)
	if err != nil {
		return cloudplaneapi.ServiceResponse{}, fmt.Errorf("get service: %w", err)
	}
	return response, nil
}

func (s *Service) buildProject(ctx context.Context, project controlproject.Project, serviceRequest *cloudplaneapi.ApplyServiceRequest) (cloudplaneapi.Project, error) {
	request := cloudplaneapi.Project{
		ID:          project.ID,
		Name:        project.Name,
		DisplayName: project.DisplayName,
		OwnerUserID: project.OwnerUserID,
		Quota:       project.Quota,
	}
	if serviceRequest == nil {
		return request, nil
	}
	collector := projectResourceCollector{}
	if serviceRequest.Spec.ConfigSetID != "" {
		collector.addConfigSet(serviceRequest.Spec.ConfigSetID)
	}
	if serviceRequest.Spec.SecretSetID != "" {
		collector.addSecretSet(serviceRequest.Spec.SecretSetID)
	}
	if serviceRequest.Spec.RegistryCredentialID != "" {
		collector.addRegistryCredential(serviceRequest.Spec.RegistryCredentialID)
	}
	for _, item := range projectedfile.CloneSpecs(serviceRequest.Spec.ProjectedFiles) {
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			collector.addConfigSet(item.SourceID)
		case projectedfile.SourceKindSecretSet:
			collector.addSecretSet(item.SourceID)
		default:
			return cloudplaneapi.Project{}, projectedfile.ErrSourceKindInvalid
		}
	}

	for _, id := range collector.configSetIDs {
		local, err := s.store.GetProjectConfigSet(ctx, project.ID, id)
		if err != nil {
			return cloudplaneapi.Project{}, err
		}
		request.ConfigSets = append(request.ConfigSets, cloudplaneapi.ProjectConfigSet{
			ID:     local.ID,
			Name:   local.Name,
			Values: local.Values,
		})
	}
	for _, id := range collector.secretSetIDs {
		local, err := s.store.GetProjectSecretSet(ctx, project.ID, id)
		if err != nil {
			return cloudplaneapi.Project{}, err
		}
		request.SecretSets = append(request.SecretSets, cloudplaneapi.ProjectSecretSet{
			ID:     local.ID,
			Name:   local.Name,
			Values: local.Values,
		})
	}
	for _, id := range collector.registryCredentialIDs {
		local, err := s.store.GetProjectRegistryCredential(ctx, project.ID, id)
		if err != nil {
			return cloudplaneapi.Project{}, err
		}
		request.RegistryCredentials = append(request.RegistryCredentials, cloudplaneapi.ProjectRegistryCredential{
			ID:       local.ID,
			Name:     local.Name,
			Server:   local.Server,
			Username: local.Username,
			Password: local.Password,
		})
	}
	return request, nil
}

type projectResourceCollector struct {
	configSetIDs           []string
	secretSetIDs           []string
	registryCredentialIDs  []string
	seenConfigSet          map[string]struct{}
	seenSecretSet          map[string]struct{}
	seenRegistryCredential map[string]struct{}
}

func (c *projectResourceCollector) addConfigSet(id string) {
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

func (c *projectResourceCollector) addSecretSet(id string) {
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

func (c *projectResourceCollector) addRegistryCredential(id string) {
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
