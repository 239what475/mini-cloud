package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type snapshotServer struct {
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer

	logger *slog.Logger
	store  *store.Store
	config cloudplaneconfig.Config
	auth   authenticator
}

func newSnapshotServer(logger *slog.Logger, stores *store.Store, cfg cloudplaneconfig.Config, auth authenticator) cloudplanev1.ControlPlaneSnapshotServiceServer {
	return &snapshotServer{logger: logger, store: stores, config: cfg, auth: auth}
}

func (s *snapshotServer) GetSnapshot(ctx context.Context, _ *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	if err := s.auth.authorize(ctx); err != nil {
		return nil, err
	}

	snapshot, err := s.collectSnapshot(ctx)
	if err != nil {
		s.logger.Error("build cloud-plane snapshot failed", "error", err)
		return nil, status.Error(codes.Internal, "build cloud-plane snapshot failed")
	}
	return snapshot, nil
}

func (s *snapshotServer) collectSnapshot(ctx context.Context) (*cloudplanev1.PlaneSnapshot, error) {
	checkedAt := time.Now().UTC()

	alertSignal, err := s.store.GetAlertSignal(ctx)
	if err != nil {
		s.logger.Error("load cloud-plane alert signal for snapshot failed", "error", err)
		return nil, fmt.Errorf("load alert signal: %w", err)
	}

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		s.logger.Error("load nodes for snapshot failed", "error", err)
		return nil, fmt.Errorf("load nodes: %w", err)
	}

	executions, err := s.store.ListExecutionSnapshots(ctx)
	if err != nil {
		s.logger.Error("load execution snapshots failed", "error", err)
		return nil, fmt.Errorf("load execution snapshots: %w", err)
	}

	services, err := s.store.ListServices(ctx)
	if err != nil {
		s.logger.Error("load services for snapshot failed", "error", err)
		return nil, fmt.Errorf("load services: %w", err)
	}

	frontdoorDomains, err := s.store.ListFrontDoorDomains(ctx)
	if err != nil {
		s.logger.Error("load frontdoor domains for snapshot failed", "error", err)
		return nil, fmt.Errorf("load frontdoor domains: %w", err)
	}

	return &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:     s.config.Plane.Name,
			Provider: s.config.Infrastructure.Provider,
			Region:   s.config.Infrastructure.RegionID,
		},
		CheckedAt: protoTimestamp(checkedAt),
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: int32(alertSignal.AlertsFiring),
		},
		NodeInventory:    protoNodeInventory(checkedAt, nodes),
		Executions:       protoExecutionSnapshots(executions),
		Services:         protoServices(services),
		FrontdoorDomains: protoFrontDoorDomains(frontdoorDomains),
	}, nil
}

func protoServices(items []cloudmodel.Service) []*cloudplanev1.PlaneService {
	out := make([]*cloudplanev1.PlaneService, 0, len(items))
	for _, item := range items {
		out = append(out, protoService(item))
	}
	return out
}

func protoService(item cloudmodel.Service) *cloudplanev1.PlaneService {
	env := make(map[string]string, len(item.Spec.Env))
	for key, value := range item.Spec.Env {
		env[key] = value
	}
	return &cloudplanev1.PlaneService{
		ServiceId:    item.ID,
		Name:         item.Name,
		DisplayName:  item.DisplayName,
		Host:         item.Host,
		Generation:   item.Generation,
		DesiredState: item.DesiredState,
		Spec: &cloudplanev1.PlaneServiceSpec{
			InstanceClass: item.Spec.InstanceClass,
			Exposure:      item.Spec.Exposure,
			Image:         item.Spec.Image,
			Command:       append([]string(nil), item.Spec.Command...),
			Args:          append([]string(nil), item.Spec.Args...),
			Env:           env,
			ContainerPort: int32(item.Spec.ContainerPort),
			ReadinessPath: item.Spec.ReadinessPath,
		},
		UpdatedAt: protoTimestamp(item.UpdatedAt),
	}
}

func protoFrontDoorDomains(items []cloudmodel.ManagedFrontDoorDomain) []*cloudplanev1.PlaneFrontDoorDomain {
	out := make([]*cloudplanev1.PlaneFrontDoorDomain, 0, len(items))
	for _, item := range items {
		protoDomain := &cloudplanev1.PlaneFrontDoorDomain{
			Host:  item.Host,
			Cname: item.CNAME,
		}
		if item.Verification != nil {
			protoDomain.VerifySubdomain = item.Verification.Subdomain
			protoDomain.VerifyType = item.Verification.Type
			protoDomain.VerifyValue = item.Verification.Value
		}
		out = append(out, protoDomain)
	}
	return out
}

func protoNodeInventory(observedAt time.Time, nodes []cloudmodel.Node) *cloudplanev1.PlaneNodeInventory {
	out := &cloudplanev1.PlaneNodeInventory{
		ObservedAt: protoTimestamp(observedAt),
		Nodes:      make([]*cloudplanev1.PlaneNode, 0, len(nodes)),
	}

	var maxUpdatedAt time.Time
	for _, item := range nodes {
		if item.Status == cloudmodel.StatusDeleted {
			continue
		}
		if item.UpdatedAt.After(maxUpdatedAt) {
			maxUpdatedAt = item.UpdatedAt
		}
		protoNode := &cloudplanev1.PlaneNode{
			NodeId:              item.ID,
			Name:                item.Name,
			Provider:            item.Provider,
			Region:              item.Region,
			InstanceId:          item.InstanceID,
			InstanceType:        item.InstanceType,
			Status:              item.Status,
			Schedulable:         item.Schedulable,
			Elastic:             item.Elastic,
			CpuMilliTotal:       int32(item.CPUMilliTotal),
			CpuMilliAllocatable: int32(item.CPUMilliAllocatable),
			CpuMilliAllocated:   int32(item.CPUMilliAllocated),
			MemoryMiTotal:       int32(item.MemoryMiTotal),
			MemoryMiAllocatable: int32(item.MemoryMiAllocatable),
			MemoryMiAllocated:   int32(item.MemoryMiAllocated),
		}
		if item.LastHeartbeatAt != nil {
			protoNode.LastHeartbeatAt = protoTimestamp(*item.LastHeartbeatAt)
		}
		out.Nodes = append(out.Nodes, protoNode)
	}
	if !maxUpdatedAt.IsZero() {
		out.ObservedAt = protoTimestamp(maxUpdatedAt)
	}
	return out
}

func protoExecutionSnapshots(items []cloudmodel.ExecutionSnapshot) []*cloudplanev1.PlaneExecutionSnapshot {
	out := make([]*cloudplanev1.PlaneExecutionSnapshot, 0, len(items))
	for _, item := range items {
		out = append(out, &cloudplanev1.PlaneExecutionSnapshot{
			PlanId:            item.PlanID,
			ServiceId:         item.ServiceID,
			ServiceName:       item.ServiceName,
			ServiceGeneration: item.ServiceGeneration,
			Status:            item.Status,
			LastStatusReason:  item.LastStatusReason,
			ObservedAt:        protoTimestamp(item.ObservedAt),
		})
	}
	return out
}

func protoTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value.UTC())
}
