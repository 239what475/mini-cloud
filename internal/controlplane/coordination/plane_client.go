package coordination

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

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

var errPlaneObjectNotFound = errors.New("plane api object not found")

type planeRPCError struct {
	Code    string
	Message string
}

func (e *planeRPCError) Error() string {
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("plane gRPC returned code %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("plane gRPC returned error: %s", e.Message)
}

type planeClient struct {
	bearerToken  string
	conn         *grpc.ClientConn
	snapshotRPC  cloudplanev1.ControlPlaneSnapshotServiceClient
	executionRPC cloudplanev1.ControlPlaneExecutionServiceClient
}

func newPlaneClient(grpcEndpoint string, bearerToken string) (*planeClient, error) {
	return newPlaneClientWithDialOptions(grpcEndpoint, bearerToken)
}

func newPlaneClientWithDialOptions(grpcEndpoint string, bearerToken string, dialOptions ...grpc.DialOption) (*planeClient, error) {
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
	return &planeClient{
		bearerToken:  strings.TrimSpace(bearerToken),
		conn:         conn,
		snapshotRPC:  cloudplanev1.NewControlPlaneSnapshotServiceClient(conn),
		executionRPC: cloudplanev1.NewControlPlaneExecutionServiceClient(conn),
	}, nil
}

func (c *planeClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *planeClient) Snapshot(ctx context.Context) (cloudplaneapi.SnapshotResponse, error) {
	resp, err := c.snapshotRPC.GetSnapshot(withAuth(ctx, c.bearerToken), &emptypb.Empty{})
	if err != nil {
		return cloudplaneapi.SnapshotResponse{}, classifyRPCError(err)
	}
	return snapshotFromProto(resp), nil
}

func (c *planeClient) ApplyExecutionPlan(ctx context.Context, input cloudplaneapi.ExecutionPlanRequest) (cloudplaneapi.ExecutionPlanResponse, error) {
	resp, err := c.executionRPC.ApplyExecutionPlan(withAuth(ctx, c.bearerToken), &cloudplanev1.ApplyExecutionPlanRequest{
		PlanId:            strings.TrimSpace(input.PlanID),
		ServiceId:         strings.TrimSpace(input.ServiceID),
		ServiceName:       strings.TrimSpace(input.ServiceName),
		ServiceGeneration: input.ServiceGeneration,
		Image:             strings.TrimSpace(input.Image),
		Command:           append([]string(nil), input.Command...),
		Args:              append([]string(nil), input.Args...),
		Env:               copyStringMap(input.Env),
		ProjectedFiles:    protoExecutionProjectedFiles(input.ProjectedFiles),
		ImageCredential:   protoExecutionImageCredential(input.ImageCredential),
		ContainerPort:     int32(input.ContainerPort),
		ReadinessPath:     strings.TrimSpace(input.ReadinessPath),
		InstanceClass:     strings.TrimSpace(input.InstanceClass),
		Exposure:          strings.TrimSpace(input.Exposure),
	})
	if err != nil {
		return cloudplaneapi.ExecutionPlanResponse{}, classifyRPCError(err)
	}
	return executionPlanResponseFromProto(resp), nil
}

func (c *planeClient) DeleteExecutionPlan(ctx context.Context, input cloudplaneapi.DeleteExecutionPlanRequest) error {
	_, err := c.executionRPC.DeleteExecutionPlan(withAuth(ctx, c.bearerToken), &cloudplanev1.DeleteExecutionPlanRequest{
		ServiceId:         strings.TrimSpace(input.ServiceID),
		ServiceGeneration: input.ServiceGeneration,
		PlanId:            strings.TrimSpace(input.PlanID),
	})
	if err != nil {
		return classifyRPCError(err)
	}
	return nil
}

func protoExecutionProjectedFiles(items []cloudplaneapi.ExecutionProjectedFile) []*cloudplanev1.ExecutionProjectedFile {
	if len(items) == 0 {
		return nil
	}
	out := make([]*cloudplanev1.ExecutionProjectedFile, 0, len(items))
	for _, item := range items {
		out = append(out, &cloudplanev1.ExecutionProjectedFile{
			MountPath: strings.TrimSpace(item.MountPath),
			Content:   item.Content,
			Mode:      item.Mode,
			Sensitive: item.Sensitive,
		})
	}
	return out
}

func protoExecutionImageCredential(item *cloudplaneapi.ExecutionImageCredential) *cloudplanev1.ExecutionImageCredential {
	if item == nil {
		return nil
	}
	return &cloudplanev1.ExecutionImageCredential{
		Server:   strings.TrimSpace(item.Server),
		Username: strings.TrimSpace(item.Username),
		Password: item.Password,
	}
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
		return fmt.Errorf("%w: %s", errPlaneObjectNotFound, st.Message())
	}
	apiErr := &planeRPCError{
		Code:    st.Code().String(),
		Message: st.Message(),
	}
	return apiErr
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
			ServicesTotal:            int(item.GetOverview().GetServicesTotal()),
			ServicesIdle:             int(item.GetOverview().GetServicesIdle()),
			ServicesDeploying:        int(item.GetOverview().GetServicesDeploying()),
			ServicesRunning:          int(item.GetOverview().GetServicesRunning()),
			ServicesDegraded:         int(item.GetOverview().GetServicesDegraded()),
			ServicesFailed:           int(item.GetOverview().GetServicesFailed()),
			NodesTotal:               int(item.GetOverview().GetNodesTotal()),
			NodesRegistering:         int(item.GetOverview().GetNodesRegistering()),
			NodesReady:               int(item.GetOverview().GetNodesReady()),
			NodesNotReady:            int(item.GetOverview().GetNodesNotReady()),
			NodesDraining:            int(item.GetOverview().GetNodesDraining()),
			NodesOffline:             int(item.GetOverview().GetNodesOffline()),
			ExecutionPlansTotal:      int(item.GetOverview().GetExecutionPlansTotal()),
			ExecutionPlansPending:    int(item.GetOverview().GetExecutionPlansPending()),
			ExecutionPlansScheduling: int(item.GetOverview().GetExecutionPlansScheduling()),
			ExecutionPlansAssigned:   int(item.GetOverview().GetExecutionPlansAssigned()),
			ExecutionPlansDeploying:  int(item.GetOverview().GetExecutionPlansDeploying()),
			ExecutionPlansRunning:    int(item.GetOverview().GetExecutionPlansRunning()),
			ExecutionPlansFailed:     int(item.GetOverview().GetExecutionPlansFailed()),
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
	out.Executions = executionSnapshotsFromProto(item.GetExecutions())
	return out
}

func executionSnapshotsFromProto(items []*cloudplanev1.PlaneExecutionSnapshot) []cloudplaneapi.ExecutionSnapshot {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.ExecutionSnapshot, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		view := cloudplaneapi.ExecutionSnapshot{
			PlanID:            item.GetPlanId(),
			ServiceID:         item.GetServiceId(),
			ServiceName:       item.GetServiceName(),
			ServiceGeneration: item.GetServiceGeneration(),
			Status:            item.GetStatus(),
			LastStatusReason:  item.GetLastStatusReason(),
		}
		if ts := item.GetObservedAt(); ts != nil {
			view.ObservedAt = ts.AsTime().UTC()
		}
		out = append(out, view)
	}
	return out
}

func executionPlanResponseFromProto(item *cloudplanev1.ApplyExecutionPlanResponse) cloudplaneapi.ExecutionPlanResponse {
	if item == nil {
		return cloudplaneapi.ExecutionPlanResponse{}
	}
	return cloudplaneapi.ExecutionPlanResponse{
		Action: item.GetAction(),
		PlanID: item.GetPlanId(),
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
