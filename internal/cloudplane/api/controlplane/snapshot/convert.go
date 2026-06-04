package snapshot

import (
	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

// protoSnapshot 将已聚合的 cloud-plane 快照 contract 转为 control-plane 同步使用的 protobuf。
// 参数说明：item 是 cloud-plane 当前 plane snapshot 的 contract 视图。
func protoSnapshot(item cloudplaneapi.SnapshotResponse) *cloudplanev1.PlaneSnapshot {
	// Plane 保存 plane 自身身份摘要，供 control-plane 判断该快照来自哪个 cloud-plane。
	return &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:       item.Plane.Name,
			Provider:   item.Plane.Provider,
			Region:     item.Plane.Region,
			Configured: item.Plane.Configured,
		},
		// Health 是 cloud-plane 本地健康检查结果；时间字段统一转为 protobuf Timestamp。
		Health: &cloudplanev1.PlaneHealth{
			CheckedAt: controlplane.ProtoTimestamp(item.Health.CheckedAt),
			Service:   item.Health.Service,
			Database:  item.Health.Database,
		},
		// Overview 是资源数量和状态分布，只做类型转换，不改变统计口径。
		Overview: &cloudplanev1.PlaneOverview{
			ServicesTotal:         int32(item.Overview.ServicesTotal),
			ServicesIdle:          int32(item.Overview.ServicesIdle),
			ServicesDeploying:     int32(item.Overview.ServicesDeploying),
			ServicesRunning:       int32(item.Overview.ServicesRunning),
			ServicesDegraded:      int32(item.Overview.ServicesDegraded),
			ServicesFailed:        int32(item.Overview.ServicesFailed),
			NodesTotal:            int32(item.Overview.NodesTotal),
			NodesRegistering:      int32(item.Overview.NodesRegistering),
			NodesReady:            int32(item.Overview.NodesReady),
			NodesNotReady:         int32(item.Overview.NodesNotReady),
			NodesDraining:         int32(item.Overview.NodesDraining),
			NodesOffline:          int32(item.Overview.NodesOffline),
			DeploymentsTotal:      int32(item.Overview.DeploymentsTotal),
			DeploymentsPending:    int32(item.Overview.DeploymentsPending),
			DeploymentsScheduling: int32(item.Overview.DeploymentsScheduling),
			DeploymentsAssigned:   int32(item.Overview.DeploymentsAssigned),
			DeploymentsDeploying:  int32(item.Overview.DeploymentsDeploying),
			DeploymentsRunning:    int32(item.Overview.DeploymentsRunning),
			DeploymentsFailed:     int32(item.Overview.DeploymentsFailed),
		},
		// Capacity 是 runtime node 容量聚合；Go 领域模型使用 int，protobuf 使用 int32。
		Capacity: &cloudplanev1.PlaneCapacity{
			RuntimeNodesTotal:   int32(item.Capacity.RuntimeNodesTotal),
			RuntimeNodesReady:   int32(item.Capacity.RuntimeNodesReady),
			CpuMilliTotal:       int32(item.Capacity.CPUMilliTotal),
			CpuMilliAllocatable: int32(item.Capacity.CPUMilliAllocatable),
			CpuMilliAllocated:   int32(item.Capacity.CPUMilliAllocated),
			MemoryMiTotal:       int32(item.Capacity.MemoryMiTotal),
			MemoryMiAllocatable: int32(item.Capacity.MemoryMiAllocatable),
			MemoryMiAllocated:   int32(item.Capacity.MemoryMiAllocated),
		},
		// Reliability 只同步 control-plane 当前需要聚合的可靠性摘要字段。
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: int32(item.Reliability.AlertsFiring),
		},
		// RuntimeInventory 和 RuntimeConfig 由专用转换函数处理嵌套结构和动态 summary。
		RuntimeInventory: protoRuntimeInventory(item.Runtime),
		RuntimeConfig:    protoRuntimeConfig(item.RuntimeConfig),
	}
}

// protoRuntimeInventory 将 runtime inventory contract 视图转为 control-plane 同步使用的 protobuf。
// 参数说明：item 是当前 plane 的 runtime node 清单快照。
func protoRuntimeInventory(item cloudplaneapi.RuntimeInventory) *cloudplanev1.PlaneRuntimeInventory {
	// 先创建带容量的输出切片，避免逐个追加 runtime node 时频繁扩容。
	out := &cloudplanev1.PlaneRuntimeInventory{
		SyncVersion: item.SyncVersion,
		ObservedAt:  controlplane.ProtoTimestamp(item.ObservedAt),
		Nodes:       make([]*cloudplanev1.PlaneRuntimeNode, 0, len(item.Nodes)),
	}
	// 逐个转换 runtime node；该循环只做字段映射，不改变调度或容量计算结果。
	for _, node := range item.Nodes {
		// protoNode 保留 control-plane 调度所需的身份、状态、调度开关和容量字段。
		protoNode := &cloudplanev1.PlaneRuntimeNode{
			NodeId:              node.NodeID,
			NodeEpoch:           node.NodeEpoch,
			Name:                node.Name,
			Provider:            node.Provider,
			Region:              node.Region,
			InstanceId:          node.InstanceID,
			InstanceType:        node.InstanceType,
			Status:              node.Status,
			Schedulable:         node.Schedulable,
			CpuMilliTotal:       int32(node.CPUMilliTotal),
			CpuMilliAllocatable: int32(node.CPUMilliAllocatable),
			CpuMilliAllocated:   int32(node.CPUMilliAllocated),
			MemoryMiTotal:       int32(node.MemoryMiTotal),
			MemoryMiAllocatable: int32(node.MemoryMiAllocatable),
			MemoryMiAllocated:   int32(node.MemoryMiAllocated),
		}
		// LastHeartbeatAt 在领域模型中可为空；只有存在心跳时间时才设置 protobuf 字段。
		if node.LastHeartbeatAt != nil {
			protoNode.LastHeartbeatAt = controlplane.ProtoTimestamp(*node.LastHeartbeatAt)
		}
		// 不在转换层排序，只保持上游传入的 runtime node 顺序。
		out.Nodes = append(out.Nodes, protoNode)
	}
	return out
}

// protoRuntimeConfig 将 runtime config 摘要转换为 protobuf 响应对象。
// 参数说明：item 是当前 plane 的 runtime config 快照。
func protoRuntimeConfig(item cloudplaneapi.RuntimeConfigSnapshot) *cloudplanev1.PlaneRuntimeConfig {
	// 基础 runtime config 快照包含观测时间和 fingerprint，供 control-plane 判断配置是否变化。
	out := &cloudplanev1.PlaneRuntimeConfig{
		ObservedAt:  controlplane.ProtoTimestamp(item.ObservedAt),
		Fingerprint: item.Fingerprint,
	}
	// Summary 是动态结构；为空时不设置 protobuf Struct，避免产生无意义的空对象。
	if len(item.Summary) > 0 {
		// structpb 转换失败时当前函数不返回错误，Summary 会被静默省略，只保留基础快照字段。
		if summary, err := structpb.NewStruct(item.Summary); err == nil {
			out.Summary = summary
		}
	}
	return out
}
