package planeclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

var ErrNotFound = errors.New("plane api object not found")

type RPCError struct {
	Code          string
	Message       string
	RejectReasons []cloudplaneapi.QuotaRejectReason
}

func (e *RPCError) Error() string {
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("plane gRPC returned code %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("plane gRPC returned error: %s", e.Message)
}

type Client struct {
	bearerToken string
	conn        *grpc.ClientConn
	snapshotRPC cloudplanev1.ControlPlaneSnapshotServiceClient
	projectRPC  cloudplanev1.ControlPlaneProjectServiceClient
	workloadRPC cloudplanev1.ControlPlaneWorkloadServiceClient
}

func New(grpcEndpoint string, bearerToken string) (*Client, error) {
	return NewWithDialOptions(grpcEndpoint, bearerToken)
}

func NewWithDialOptions(grpcEndpoint string, bearerToken string, dialOptions ...grpc.DialOption) (*Client, error) {
	target, transportCredentials, err := resolveTarget(grpcEndpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("bearerToken is required")
	}

	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(transportCredentials)}, dialOptions...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial plane service: %w", err)
	}
	return &Client{
		bearerToken: strings.TrimSpace(bearerToken),
		conn:        conn,
		snapshotRPC: cloudplanev1.NewControlPlaneSnapshotServiceClient(conn),
		projectRPC:  cloudplanev1.NewControlPlaneProjectServiceClient(conn),
		workloadRPC: cloudplanev1.NewControlPlaneWorkloadServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Snapshot(ctx context.Context) (cloudplaneapi.SnapshotResponse, error) {
	resp, err := c.snapshotRPC.GetSnapshot(withAuth(ctx, c.bearerToken), &emptypb.Empty{})
	if err != nil {
		return cloudplaneapi.SnapshotResponse{}, classifyRPCError(err)
	}
	return snapshotFromProto(resp), nil
}

func (c *Client) ApplyProject(ctx context.Context, project cloudplaneapi.Project) (cloudplaneapi.ApplyProjectResponse, error) {
	resp, err := c.projectRPC.ApplyProject(withAuth(ctx, c.bearerToken), &cloudplanev1.ApplyProjectRequest{
		ProjectId:           strings.TrimSpace(project.ID),
		Name:                project.Name,
		DisplayName:         project.DisplayName,
		OwnerUserId:         project.OwnerUserID,
		ConfigSets:          protoProjectConfigSets(project.ConfigSets),
		SecretSets:          protoProjectSecretSets(project.SecretSets),
		RegistryCredentials: protoProjectRegistryCredentials(project.RegistryCredentials),
		Quota: &cloudplanev1.ProjectQuota{
			MaxServices: int32(project.Quota.MaxServices),
			CpuMilli:    int32(project.Quota.CPUMilli),
			MemoryMi:    int32(project.Quota.MemoryMi),
		},
	})
	if err != nil {
		return cloudplaneapi.ApplyProjectResponse{}, classifyRPCError(err)
	}
	return applyProjectResponseFromProto(resp), nil
}

func protoProjectConfigSets(items []cloudplaneapi.ProjectConfigSet) []*cloudplanev1.ProjectConfigSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.ProjectConfigSet, 0, len(items))
	for _, item := range items {
		out = append(out, &cloudplanev1.ProjectConfigSet{
			Id:     strings.TrimSpace(item.ID),
			Name:   strings.TrimSpace(item.Name),
			Values: copyStringMap(item.Values),
		})
	}
	return out
}

func protoProjectSecretSets(items []cloudplaneapi.ProjectSecretSet) []*cloudplanev1.ProjectSecretSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.ProjectSecretSet, 0, len(items))
	for _, item := range items {
		out = append(out, &cloudplanev1.ProjectSecretSet{
			Id:     strings.TrimSpace(item.ID),
			Name:   strings.TrimSpace(item.Name),
			Values: copyStringMap(item.Values),
		})
	}
	return out
}

func protoProjectRegistryCredentials(items []cloudplaneapi.ProjectRegistryCredential) []*cloudplanev1.ProjectRegistryCredential {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.ProjectRegistryCredential, 0, len(items))
	for _, item := range items {
		out = append(out, &cloudplanev1.ProjectRegistryCredential{
			Id:       strings.TrimSpace(item.ID),
			Name:     strings.TrimSpace(item.Name),
			Server:   strings.TrimSpace(item.Server),
			Username: strings.TrimSpace(item.Username),
			Password: item.Password,
		})
	}
	return out
}

func (c *Client) ApplyService(ctx context.Context, projectID string, serviceID string, serviceName string, input cloudplaneapi.ApplyServiceRequest) (cloudplaneapi.ApplyServiceResponse, error) {
	resp, err := c.workloadRPC.ApplyService(withAuth(ctx, c.bearerToken), &cloudplanev1.ApplyServiceRequest{
		ProjectId:   strings.TrimSpace(projectID),
		ServiceId:   strings.TrimSpace(serviceID),
		Name:        strings.TrimSpace(serviceName),
		DisplayName: input.DisplayName,
		Spec: &cloudplanev1.ServiceSpec{
			Region:               input.Spec.Region,
			Replicas:             int32(input.Spec.Replicas),
			InstanceClass:        input.Spec.InstanceClass,
			Exposure:             input.Spec.Exposure,
			Image:                input.Spec.Image,
			Command:              append([]string(nil), input.Spec.Command...),
			Args:                 append([]string(nil), input.Spec.Args...),
			DefaultPort:          int32(input.Spec.DefaultPort),
			ReadinessPath:        input.Spec.ReadinessPath,
			Env:                  copyStringMap(input.Spec.Env),
			ConfigSetId:          input.Spec.ConfigSetID,
			SecretSetId:          input.Spec.SecretSetID,
			RegistryCredentialId: input.Spec.RegistryCredentialID,
			ProjectedFiles:       protoServiceProjectedFiles(input.Spec.ProjectedFiles),
			PersistentDirs:       protoServicePersistentDirs(input.Spec.PersistentDirs),
		},
	})
	if err != nil {
		return cloudplaneapi.ApplyServiceResponse{}, classifyRPCError(err)
	}
	return applyServiceResponseFromProto(resp), nil
}

func (c *Client) DeleteService(ctx context.Context, projectID string, serviceID string) error {
	_, err := c.workloadRPC.DeleteService(withAuth(ctx, c.bearerToken), &cloudplanev1.DeleteServiceRequest{
		ProjectId: strings.TrimSpace(projectID),
		ServiceId: strings.TrimSpace(serviceID),
	})
	if err != nil {
		return classifyRPCError(err)
	}
	return nil
}

func (c *Client) GetService(ctx context.Context, projectID string, serviceID string) (cloudplaneapi.ServiceResponse, error) {
	resp, err := c.workloadRPC.GetService(withAuth(ctx, c.bearerToken), &cloudplanev1.GetServiceRequest{
		ProjectId: strings.TrimSpace(projectID),
		ServiceId: strings.TrimSpace(serviceID),
	})
	if err != nil {
		return cloudplaneapi.ServiceResponse{}, classifyRPCError(err)
	}
	return getServiceResponseFromProto(resp), nil
}

func protoServiceProjectedFiles(items []projectedfile.Spec) []*cloudplanev1.ProjectedFileSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.ProjectedFileSpec, 0, len(items))
	for _, item := range projectedfile.CloneSpecs(items) {
		out = append(out, &cloudplanev1.ProjectedFileSpec{
			MountPath:  item.MountPath,
			SourceKind: string(item.SourceKind),
			SourceId:   item.SourceID,
			SourceKey:  item.SourceKey,
		})
	}
	return out
}

func protoServicePersistentDirs(items []persistentdir.Spec) []*cloudplanev1.PersistentDirSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.PersistentDirSpec, 0, len(items))
	for _, item := range persistentdir.CloneSpecs(items) {
		out = append(out, &cloudplanev1.PersistentDirSpec{
			Name:      item.Name,
			MountPath: item.MountPath,
		})
	}
	return out
}

func resolveTarget(grpcEndpoint string) (string, credentials.TransportCredentials, error) {
	trimmedGRPCEndpoint := strings.TrimSpace(grpcEndpoint)
	if trimmedGRPCEndpoint == "" {
		return "", nil, fmt.Errorf("grpcEndpoint is required")
	}
	if strings.ContainsAny(trimmedGRPCEndpoint, " \t\r\n") {
		return "", nil, fmt.Errorf("grpcEndpoint must not contain whitespace")
	}
	lower := strings.ToLower(trimmedGRPCEndpoint)
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		return "", nil, fmt.Errorf("grpcEndpoint must be a gRPC target, not an HTTP URL")
	case strings.HasPrefix(lower, "grpc://"):
		target := strings.TrimSpace(trimmedGRPCEndpoint[len("grpc://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", nil, fmt.Errorf("grpcEndpoint must include host:port")
		}
		return target, insecure.NewCredentials(), nil
	case strings.HasPrefix(lower, "grpcs://"):
		target := strings.TrimSpace(trimmedGRPCEndpoint[len("grpcs://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", nil, fmt.Errorf("grpcEndpoint must include host:port")
		}
		return target, credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
	default:
		return trimmedGRPCEndpoint, insecure.NewCredentials(), nil
	}
}

func withAuth(ctx context.Context, bearerToken string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+strings.TrimSpace(bearerToken))
}

func classifyRPCError(err error) error {
	st := status.Convert(err)
	if st.Code() == codes.NotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, st.Message())
	}
	apiErr := &RPCError{
		Code:    st.Code().String(),
		Message: st.Message(),
	}
	for _, detail := range st.Details() {
		rejected, ok := detail.(*cloudplanev1.QuotaAdmissionRejected)
		if !ok {
			continue
		}
		apiErr.RejectReasons = quotaRejectReasonsFromProto(rejected.GetRejectReasons())
		break
	}
	return apiErr
}

func quotaRejectReasonsFromProto(items []*cloudplanev1.QuotaRejectReason) []cloudplaneapi.QuotaRejectReason {
	out := make([]cloudplaneapi.QuotaRejectReason, 0, len(items))
	for _, item := range items {
		out = append(out, cloudplaneapi.QuotaRejectReason{
			Code:           item.GetCode(),
			Message:        item.GetMessage(),
			Current:        item.GetCurrent(),
			RequestedDelta: item.GetRequestedDelta(),
			Projected:      item.GetProjected(),
			Limit:          item.GetLimit(),
		})
	}
	return out
}

func snapshotFromProto(item *cloudplanev1.PlaneSnapshot) cloudplaneapi.SnapshotResponse {
	if item == nil {
		return cloudplaneapi.SnapshotResponse{}
	}
	out := cloudplaneapi.SnapshotResponse{}
	if item.GetPlane() != nil {
		out.Plane = cloudplaneapi.PlaneSummary{
			Name:       item.GetPlane().GetName(),
			Provider:   item.GetPlane().GetProvider(),
			Region:     item.GetPlane().GetRegion(),
			Configured: item.GetPlane().GetConfigured(),
		}
	}
	if item.GetHealth() != nil {
		out.Health = cloudplaneapi.HealthSummary{
			CheckedAt: item.GetHealth().GetCheckedAt().AsTime(),
			Service:   item.GetHealth().GetService(),
			Database:  item.GetHealth().GetDatabase(),
		}
	}
	if item.GetOverview() != nil {
		out.Overview = cloudplaneapi.OverviewSummary{
			ProjectsTotal:         int(item.GetOverview().GetProjectsTotal()),
			ServicesTotal:         int(item.GetOverview().GetServicesTotal()),
			ServicesIdle:          int(item.GetOverview().GetServicesIdle()),
			ServicesDeploying:     int(item.GetOverview().GetServicesDeploying()),
			ServicesRunning:       int(item.GetOverview().GetServicesRunning()),
			ServicesDegraded:      int(item.GetOverview().GetServicesDegraded()),
			ServicesFailed:        int(item.GetOverview().GetServicesFailed()),
			NodesTotal:            int(item.GetOverview().GetNodesTotal()),
			NodesRegistering:      int(item.GetOverview().GetNodesRegistering()),
			NodesReady:            int(item.GetOverview().GetNodesReady()),
			NodesNotReady:         int(item.GetOverview().GetNodesNotReady()),
			NodesDraining:         int(item.GetOverview().GetNodesDraining()),
			NodesOffline:          int(item.GetOverview().GetNodesOffline()),
			DeploymentsTotal:      int(item.GetOverview().GetDeploymentsTotal()),
			DeploymentsPending:    int(item.GetOverview().GetDeploymentsPending()),
			DeploymentsScheduling: int(item.GetOverview().GetDeploymentsScheduling()),
			DeploymentsAssigned:   int(item.GetOverview().GetDeploymentsAssigned()),
			DeploymentsDeploying:  int(item.GetOverview().GetDeploymentsDeploying()),
			DeploymentsRunning:    int(item.GetOverview().GetDeploymentsRunning()),
			DeploymentsFailed:     int(item.GetOverview().GetDeploymentsFailed()),
		}
	}
	if item.GetCapacity() != nil {
		out.Capacity = cloudplaneapi.CapacitySummary{
			RuntimeNodesTotal:   int(item.GetCapacity().GetRuntimeNodesTotal()),
			RuntimeNodesReady:   int(item.GetCapacity().GetRuntimeNodesReady()),
			CPUMilliTotal:       int(item.GetCapacity().GetCpuMilliTotal()),
			CPUMilliAllocatable: int(item.GetCapacity().GetCpuMilliAllocatable()),
			CPUMilliAllocated:   int(item.GetCapacity().GetCpuMilliAllocated()),
			MemoryMiTotal:       int(item.GetCapacity().GetMemoryMiTotal()),
			MemoryMiAllocatable: int(item.GetCapacity().GetMemoryMiAllocatable()),
			MemoryMiAllocated:   int(item.GetCapacity().GetMemoryMiAllocated()),
		}
	}
	if item.GetReliability() != nil {
		out.Reliability = cloudplaneapi.ReliabilitySummary{
			AlertsFiring: int(item.GetReliability().GetAlertsFiring()),
		}
	}
	if item.GetRuntimeInventory() != nil {
		out.Runtime = cloudplaneapi.RuntimeInventory{
			SyncVersion: item.GetRuntimeInventory().GetSyncVersion(),
			ObservedAt:  item.GetRuntimeInventory().GetObservedAt().AsTime(),
			Nodes:       runtimeNodesFromProto(item.GetRuntimeInventory().GetNodes()),
		}
	}
	if item.GetRuntimeConfig() != nil {
		out.RuntimeConfig = cloudplaneapi.RuntimeConfigSnapshot{
			ObservedAt:  item.GetRuntimeConfig().GetObservedAt().AsTime(),
			Fingerprint: item.GetRuntimeConfig().GetFingerprint(),
		}
		if item.GetRuntimeConfig().GetSummary() != nil {
			out.RuntimeConfig.Summary = item.GetRuntimeConfig().GetSummary().AsMap()
		}
	}
	return out
}

func applyProjectResponseFromProto(item *cloudplanev1.ApplyProjectResponse) cloudplaneapi.ApplyProjectResponse {
	if item == nil {
		return cloudplaneapi.ApplyProjectResponse{}
	}
	return cloudplaneapi.ApplyProjectResponse{
		Action:    item.GetAction(),
		ProjectID: item.GetProjectId(),
	}
}

func applyServiceResponseFromProto(item *cloudplanev1.ApplyServiceResponse) cloudplaneapi.ApplyServiceResponse {
	if item == nil {
		return cloudplaneapi.ApplyServiceResponse{}
	}
	return cloudplaneapi.ApplyServiceResponse{
		Action:            item.GetAction(),
		DesiredGeneration: item.GetDesiredGeneration(),
	}
}

func getServiceResponseFromProto(item *cloudplanev1.GetServiceResponse) cloudplaneapi.ServiceResponse {
	if item == nil {
		return cloudplaneapi.ServiceResponse{}
	}
	return cloudplaneapi.ServiceResponse{
		Service: serviceFromProto(item.GetService()),
		Status:  observedServiceStatusFromProto(item.GetStatus()),
	}
}

func serviceFromProto(item *cloudplanev1.Service) cloudplaneapi.Service {
	if item == nil {
		return cloudplaneapi.Service{}
	}
	metadata := item.GetMetadata()
	spec := item.GetSpec()
	status := item.GetStatus()
	return cloudplaneapi.Service{
		Metadata: cloudplaneapi.ServiceMetadata{
			ID:          metadata.GetId(),
			ProjectID:   metadata.GetProjectId(),
			Name:        metadata.GetName(),
			DisplayName: metadata.GetDisplayName(),
		},
		Spec: cloudplaneapi.ServiceSpec{
			Region:               spec.GetRegion(),
			Replicas:             int(spec.GetReplicas()),
			InstanceClass:        spec.GetInstanceClass(),
			Exposure:             spec.GetExposure(),
			Image:                spec.GetImage(),
			Command:              append([]string(nil), spec.GetCommand()...),
			Args:                 append([]string(nil), spec.GetArgs()...),
			DefaultPort:          int(spec.GetDefaultPort()),
			ReadinessPath:        spec.GetReadinessPath(),
			Env:                  copyStringMap(spec.GetEnv()),
			ConfigSetID:          spec.GetConfigSetId(),
			SecretSetID:          spec.GetSecretSetId(),
			RegistryCredentialID: spec.GetRegistryCredentialId(),
			ProjectedFiles:       projectedFilesFromAcceptedProto(spec.GetProjectedFiles()),
			PersistentDirs:       persistentDirsFromAcceptedProto(spec.GetPersistentDirs()),
		},
		Status: cloudplaneapi.ServiceStatus{
			Phase:               status.GetPhase(),
			CurrentRevisionID:   status.GetCurrentRevisionId(),
			CandidateRevisionID: status.GetCandidateRevisionId(),
			RolloutPhase:        status.GetRolloutPhase(),
			RolloutMessage:      status.GetRolloutMessage(),
		},
	}
}

func projectedFilesFromAcceptedProto(items []*cloudplanev1.ProjectedFileSpec) []projectedfile.Spec {
	if len(items) == 0 {
		return nil
	}
	out := make([]projectedfile.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, projectedfile.Spec{
			MountPath:  item.GetMountPath(),
			SourceKind: (item.GetSourceKind()),
			SourceID:   item.GetSourceId(),
			SourceKey:  item.GetSourceKey(),
		})
	}
	return projectedfile.CloneSpecs(out)
}

func persistentDirsFromAcceptedProto(items []*cloudplanev1.PersistentDirSpec) []persistentdir.Spec {
	if len(items) == 0 {
		return nil
	}
	out := make([]persistentdir.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, persistentdir.Spec{
			Name:      item.GetName(),
			MountPath: item.GetMountPath(),
		})
	}
	return persistentdir.CloneSpecs(out)
}

func observedServiceStatusFromProto(item *cloudplanev1.ObservedServiceStatus) cloudplaneapi.ObservedServiceStatus {
	if item == nil {
		return cloudplaneapi.ObservedServiceStatus{}
	}
	rollout := cloudplaneapi.ObservedRolloutStatus{}
	if item.GetRollout() != nil {
		rollout = cloudplaneapi.ObservedRolloutStatus{
			Phase:                      item.GetRollout().GetPhase(),
			Message:                    item.GetRollout().GetMessage(),
			StableRevisionID:           item.GetRollout().GetStableRevisionId(),
			CandidateRevisionID:        item.GetRollout().GetCandidateRevisionId(),
			StableDesiredReplicas:      int(item.GetRollout().GetStableDesiredReplicas()),
			StableReadyReplicas:        int(item.GetRollout().GetStableReadyReplicas()),
			StableAvailableReplicas:    int(item.GetRollout().GetStableAvailableReplicas()),
			CandidateDesiredReplicas:   int(item.GetRollout().GetCandidateDesiredReplicas()),
			CandidateReadyReplicas:     int(item.GetRollout().GetCandidateReadyReplicas()),
			CandidateAvailableReplicas: int(item.GetRollout().GetCandidateAvailableReplicas()),
		}
		if ts := item.GetRollout().GetObservedAt(); ts != nil {
			rollout.ObservedAt = ts.AsTime().UTC()
		}
	}
	return cloudplaneapi.ObservedServiceStatus{
		CurrentRevisionID: item.GetCurrentRevisionId(),
		Healthy:           item.GetHealthy(),
		Message:           item.GetMessage(),
		Rollout:           rollout,
	}
}

func runtimeNodesFromProto(items []*cloudplanev1.PlaneRuntimeNode) []cloudplaneapi.RuntimeNodeView {
	out := make([]cloudplaneapi.RuntimeNodeView, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		view := cloudplaneapi.RuntimeNodeView{
			NodeID:              item.GetNodeId(),
			NodeEpoch:           item.GetNodeEpoch(),
			Name:                item.GetName(),
			Provider:            item.GetProvider(),
			Region:              item.GetRegion(),
			InstanceID:          item.GetInstanceId(),
			InstanceType:        item.GetInstanceType(),
			Status:              item.GetStatus(),
			Schedulable:         item.GetSchedulable(),
			CPUMilliTotal:       int(item.GetCpuMilliTotal()),
			CPUMilliAllocatable: int(item.GetCpuMilliAllocatable()),
			CPUMilliAllocated:   int(item.GetCpuMilliAllocated()),
			MemoryMiTotal:       int(item.GetMemoryMiTotal()),
			MemoryMiAllocatable: int(item.GetMemoryMiAllocatable()),
			MemoryMiAllocated:   int(item.GetMemoryMiAllocated()),
		}
		if ts := item.GetLastHeartbeatAt(); ts != nil {
			value := ts.AsTime()
			view.LastHeartbeatAt = &value
		}
		out = append(out, view)
	}
	return out
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
