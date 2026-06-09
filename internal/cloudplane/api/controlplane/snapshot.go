package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/common/logctx"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const dbPingTimeout = 5 * time.Second

var errDatabaseUnavailable = errors.New("database unavailable")

type SnapshotServer struct {
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer

	logger *slog.Logger
	db     *sql.DB
	store  *store.Store
	config cloudplaneconfig.Config
	auth   Authenticator
}

func NewSnapshotServer(logger *slog.Logger, db *sql.DB, stores *store.Store, cfg cloudplaneconfig.Config, auth Authenticator) cloudplanev1.ControlPlaneSnapshotServiceServer {
	return &SnapshotServer{logger: logger, db: db, store: stores, config: cfg, auth: auth}
}

func (s *SnapshotServer) GetSnapshot(ctx context.Context, _ *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	snapshot, err := s.collectSnapshot(ctx)
	if err != nil {
		if errors.Is(err, errDatabaseUnavailable) {
			return nil, status.Error(codes.Unavailable, "database ping failed")
		}
		s.logger.Error("build cloud-plane snapshot failed", "error", err)
		return nil, status.Error(codes.Internal, "build cloud-plane snapshot failed")
	}
	return snapshot, nil
}

func (s *SnapshotServer) collectSnapshot(ctx context.Context) (*cloudplanev1.PlaneSnapshot, error) {
	logger := logctx.Logger(ctx, s.logger)
	checkedAt := time.Now().UTC()

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := s.db.PingContext(pingCtx); err != nil {
		logger.Error("cloud-plane snapshot database ping failed", "error", err)
		return nil, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}

	alertSignal, err := s.store.GetAlertSignal(ctx)
	if err != nil {
		logger.Error("load cloud-plane alert signal for snapshot failed", "error", err)
		return nil, fmt.Errorf("load alert signal: %w", err)
	}

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		logger.Error("load nodes for snapshot failed", "error", err)
		return nil, fmt.Errorf("load nodes: %w", err)
	}

	executions, err := s.store.ListExecutionSnapshots(ctx)
	if err != nil {
		logger.Error("load execution snapshots failed", "error", err)
		return nil, fmt.Errorf("load execution snapshots: %w", err)
	}

	runtimeConfig, err := s.runtimeConfigSnapshot(checkedAt)
	if err != nil {
		logger.Error("build cloud-plane runtime config snapshot failed", "error", err)
		return nil, fmt.Errorf("build runtime config snapshot: %w", err)
	}

	return &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:       s.config.Plane.Name,
			Provider:   s.config.Infrastructure.Provider,
			Region:     s.config.Infrastructure.RegionID,
			Configured: true,
		},
		Health: &cloudplanev1.PlaneHealth{
			CheckedAt: protoTimestamp(checkedAt),
			Service:   "ok",
			Database:  "ok",
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: int32(alertSignal.AlertsFiring),
		},
		RuntimeInventory: protoRuntimeInventory(checkedAt, nodes),
		RuntimeConfig:    runtimeConfig,
		Executions:       protoExecutionSnapshots(executions),
	}, nil
}

func protoRuntimeInventory(observedAt time.Time, nodes []cloudmodel.Node) *cloudplanev1.PlaneRuntimeInventory {
	out := &cloudplanev1.PlaneRuntimeInventory{
		SyncVersion: observedAt.UTC().UnixMicro(),
		ObservedAt:  protoTimestamp(observedAt),
		Nodes:       make([]*cloudplanev1.PlaneRuntimeNode, 0, len(nodes)),
	}

	var maxUpdatedAt time.Time
	for _, item := range nodes {
		if item.UpdatedAt.After(maxUpdatedAt) {
			maxUpdatedAt = item.UpdatedAt
		}
		protoNode := &cloudplanev1.PlaneRuntimeNode{
			NodeId:              item.ID,
			NodeEpoch:           1,
			Name:                item.Name,
			Provider:            item.Provider,
			Region:              item.Region,
			InstanceId:          item.InstanceID,
			InstanceType:        item.InstanceType,
			Status:              item.Status,
			Schedulable:         item.Schedulable,
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
		out.SyncVersion = maxUpdatedAt.UTC().UnixMicro()
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

func (s *SnapshotServer) runtimeConfigSnapshot(observedAt time.Time) (*cloudplanev1.PlaneRuntimeConfig, error) {
	summary := map[string]any{
		"observedAt": observedAt.UTC().Format(time.RFC3339Nano),
		"plane": map[string]any{
			"name": strings.TrimSpace(s.config.Plane.Name),
		},
		"provider": map[string]any{
			"name":     strings.TrimSpace(s.config.Infrastructure.Provider),
			"regionId": strings.TrimSpace(s.config.Infrastructure.RegionID),
			"zoneId":   strings.TrimSpace(s.config.Infrastructure.ZoneID),
		},
		"nodeAgent": map[string]any{
			"connectEndpoint":          strings.TrimSpace(s.config.NodeAgent.ConnectEndpoint),
			"bootstrapTokenConfigured": strings.TrimSpace(s.config.NodeAgent.BootstrapToken) != "",
			"binaryUrl":                strings.TrimSpace(s.config.NodeAgent.BinaryURL),
		},
		"runtimeProvisioning": map[string]any{
			"providerSpecConfigured": len(s.config.RuntimeProvisioning.ProviderSpec) > 0,
			"registryMirrorsCount":   len(s.config.RuntimeProvisioning.RegistryMirrors),
			"egressProxyConfigured":  strings.TrimSpace(s.config.RuntimeProvisioning.EgressProxyEndpoint) != "",
		},
		"ingress": map[string]any{
			"configured": strings.TrimSpace(s.config.Ingress.BaseDomain) != "",
			"baseDomain": strings.TrimSpace(s.config.Ingress.BaseDomain),
		},
		"observability": map[string]any{
			"logsConfigured":   strings.TrimSpace(s.config.Observability.LokiURL) != "",
			"tracesConfigured": strings.TrimSpace(s.config.Observability.OTLPEndpoint) != "",
		},
	}
	summary["fingerprint"] = runtimeConfigFingerprint(summary)
	protoSummary, err := structpb.NewStruct(summary)
	if err != nil {
		return nil, err
	}
	return &cloudplanev1.PlaneRuntimeConfig{
		ObservedAt:  protoTimestamp(observedAt),
		Fingerprint: summary["fingerprint"].(string),
		Summary:     protoSummary,
	}, nil
}

func runtimeConfigFingerprint(summary map[string]any) string {
	payload := make(map[string]any, len(summary))
	for key, value := range summary {
		if key == "observedAt" || key == "fingerprint" {
			continue
		}
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
