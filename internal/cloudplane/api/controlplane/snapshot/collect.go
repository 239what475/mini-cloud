package snapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/contract/cloudplaneapi"
)

const dbPingTimeout = 5 * time.Second

// errDatabaseUnavailable 表示 snapshot 聚合时数据库健康探测失败。
var errDatabaseUnavailable = errors.New("database unavailable")

// collectSnapshot 聚合 cloud-plane 当前健康、overview、容量、可靠性、runtime inventory 和配置摘要。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Server) collectSnapshot(ctx context.Context) (cloudplaneapi.SnapshotResponse, error) {
	// 阶段一：用独立短 timeout 探测数据库，避免 snapshot 请求被数据库健康检查长期阻塞。
	// 阶段二：读取本地 overview、reliability、node 清单和配置摘要，形成统一 contract 快照。
	// 阶段三：只返回聚合视图，不返回 project/service/deployment 明细，避免 southbound 同步接口承担查询 API 职责。
	logger := logctx.Logger(ctx, s.logger)
	checkedAt := time.Now().UTC()
	health := cloudplaneapi.HealthSummary{CheckedAt: checkedAt, Service: "ok", Database: "ok"}

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := s.db.PingContext(pingCtx); err != nil {
		logger.Error("cloud-plane snapshot database ping failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}

	overview, err := s.store.GetPlatformOverview(ctx)
	if err != nil {
		logger.Error("load cloud-plane overview for snapshot failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("load platform overview: %w", err)
	}

	reliability, err := s.store.GetPlatformReliabilitySnapshot(ctx)
	if err != nil {
		logger.Error("load cloud-plane reliability for snapshot failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("load platform reliability: %w", err)
	}

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		logger.Error("load runtime nodes for snapshot failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("load nodes: %w", err)
	}

	executions, err := s.store.ListExecutionSnapshots(ctx)
	if err != nil {
		logger.Error("load execution snapshots failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("load execution snapshots: %w", err)
	}

	runtimeSummary := cloudplaneconfig.BuildSummary(s.config, checkedAt)
	runtimeSummaryMap, err := cloudplaneconfig.SummaryMap(runtimeSummary)
	if err != nil {
		logger.Error("build cloud-plane runtime config summary failed", "error", err)
		return cloudplaneapi.SnapshotResponse{}, fmt.Errorf("build runtime config summary: %w", err)
	}

	alertsFiring := 0
	for _, alert := range reliability.Alerts {
		if alert.State == "firing" {
			alertsFiring++
		}
	}

	return cloudplaneapi.SnapshotResponse{
		Plane: cloudplaneapi.PlaneSummary{
			Name:       s.config.Plane.Identity.Name,
			Provider:   s.config.Infrastructure.Provider,
			Region:     s.config.Infrastructure.Location.RegionID,
			Configured: true,
		},
		Health: health,
		Overview: cloudplaneapi.OverviewSummary{
			ServicesTotal:         overview.ServicesTotal,
			ServicesIdle:          overview.ServicesIdle,
			ServicesDeploying:     overview.ServicesDeploying,
			ServicesRunning:       overview.ServicesRunning,
			ServicesDegraded:      overview.ServicesDegraded,
			ServicesFailed:        overview.ServicesFailed,
			NodesTotal:            overview.NodesTotal,
			NodesRegistering:      overview.NodesRegistering,
			NodesReady:            overview.NodesReady,
			NodesNotReady:         overview.NodesNotReady,
			NodesDraining:         overview.NodesDraining,
			NodesOffline:          overview.NodesOffline,
			DeploymentsTotal:      overview.DeploymentsTotal,
			DeploymentsPending:    overview.DeploymentsPending,
			DeploymentsScheduling: overview.DeploymentsScheduling,
			DeploymentsAssigned:   overview.DeploymentsAssigned,
			DeploymentsDeploying:  overview.DeploymentsDeploying,
			DeploymentsRunning:    overview.DeploymentsRunning,
			DeploymentsFailed:     overview.DeploymentsFailed,
		},
		Capacity: summarizeCapacity(nodes),
		Reliability: cloudplaneapi.ReliabilitySummary{
			AlertsFiring: alertsFiring,
		},
		Runtime: buildRuntimeInventory(checkedAt, nodes),
		RuntimeConfig: cloudplaneapi.RuntimeConfigSnapshot{
			ObservedAt:  runtimeSummary.ObservedAt,
			Fingerprint: runtimeSummary.Fingerprint,
			Summary:     runtimeSummaryMap,
		},
		Executions: executions,
	}, nil
}

// buildRuntimeInventory 构建 control-plane 同步用的 runtime node 清单。
// 参数说明：observedAt 是本次快照观测时间；nodes 是 cloud-plane 本地全部 node 记录。
func buildRuntimeInventory(observedAt time.Time, nodes []node.Node) cloudplaneapi.RuntimeInventory {
	// 默认 syncVersion 使用本次观测时间，确保即使没有 runtime node 也返回可比较的版本值。
	out := cloudplaneapi.RuntimeInventory{
		SyncVersion: observedAt.UTC().UnixMicro(),
		ObservedAt:  observedAt.UTC(),
		Nodes:       make([]cloudplaneapi.RuntimeNodeView, 0, len(nodes)),
	}

	// maxUpdatedAt 用于让 inventory syncVersion 反映参与同步的 runtime node 最新更新时间。
	var maxUpdatedAt time.Time
	for _, item := range nodes {
		// runtime inventory 只暴露 runtime node，不把 platform node 纳入 control-plane selection 基线。
		if item.ResolvedRole() != node.RoleRuntime {
			continue
		}
		// 记录最新 runtime node 更新时间，后面用它覆盖默认 syncVersion。
		if item.UpdatedAt.After(maxUpdatedAt) {
			maxUpdatedAt = item.UpdatedAt
		}
		// NodeEpoch 当前仍是占位版本值；真正节点版本语义以后需要由 node 状态模型提供。
		out.Nodes = append(out.Nodes, cloudplaneapi.RuntimeNodeView{
			NodeID:              item.ID,
			NodeEpoch:           1,
			Name:                item.Name,
			Provider:            item.Provider,
			Region:              item.Region,
			InstanceID:          item.InstanceID,
			InstanceType:        item.InstanceType,
			Status:              item.Status,
			Schedulable:         item.Schedulable,
			CPUMilliTotal:       item.CPUMilliTotal,
			CPUMilliAllocatable: item.CPUMilliAllocatable,
			CPUMilliAllocated:   item.CPUMilliAllocated,
			MemoryMiTotal:       item.MemoryMiTotal,
			MemoryMiAllocatable: item.MemoryMiAllocatable,
			MemoryMiAllocated:   item.MemoryMiAllocated,
			LastHeartbeatAt:     item.LastHeartbeatAt,
		})
	}
	// 如果存在 runtime node，则用最新 UpdatedAt 作为 inventory syncVersion 和 ObservedAt。
	if !maxUpdatedAt.IsZero() {
		out.SyncVersion = maxUpdatedAt.UTC().UnixMicro()
		out.ObservedAt = maxUpdatedAt.UTC()
	}
	return out
}

// summarizeCapacity 汇总 control-plane 同步用的 runtime node 容量视图。
// 参数说明：nodes 是 cloud-plane 本地全部 node 记录。
func summarizeCapacity(nodes []node.Node) cloudplaneapi.CapacitySummary {
	// 从空汇总开始，逐个累加 runtime node 的容量和分配量。
	out := cloudplaneapi.CapacitySummary{}
	for _, item := range nodes {
		// capacity 只统计承担业务调度的 runtime 节点；platform node 不计入供给侧容量。
		if item.ResolvedRole() != node.RoleRuntime {
			continue
		}
		out.RuntimeNodesTotal++
		if item.Status == node.StatusReady {
			out.RuntimeNodesReady++
		}
		// total/allocatable/allocated 都按 cloud-plane 当前 node 记录直接求和。
		out.CPUMilliTotal += item.CPUMilliTotal
		out.CPUMilliAllocatable += item.CPUMilliAllocatable
		out.CPUMilliAllocated += item.CPUMilliAllocated
		out.MemoryMiTotal += item.MemoryMiTotal
		out.MemoryMiAllocatable += item.MemoryMiAllocatable
		out.MemoryMiAllocated += item.MemoryMiAllocated
	}
	return out
}
