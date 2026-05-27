package lifecycle

import (
	"context"

	deploymentmodel "mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/workload"
)

// pickCandidateNodes 选择 candidate nodes。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 记录；nodes 是待调度或待转换的节点列表；cpuMilliRequest 是单副本 CPU 请求，单位 millicore；memoryMiRequest 是单副本内存请求，单位 MiB。
func (s deploymentCoordinator) pickCandidateNodes(ctx context.Context, serviceItem workload.Service, nodes []node.Node, cpuMilliRequest int, memoryMiRequest int) ([]node.Node, bool, error) {
	// 有 persistent dir 的 service 需要尽量回到既有 node-local 目录所在节点，避免本地目录漂移。
	if len(serviceItem.Spec.PersistentDirs) > 0 {
		pinnedNodeID, ok, err := s.resolvePersistentDirPinnedNode(ctx, serviceItem)
		if err != nil {
			return nil, false, err
		}
		// 找到 pinned node 后只保留该节点；如果容量不足，filter 会返回空列表。
		if ok {
			return filterNodesByPinnedNode(nodes, pinnedNodeID, cpuMilliRequest, memoryMiRequest), true, nil
		}
	}
	// 没有 current revision 的首次发布不需要做安全切流固定，直接使用全部候选节点。
	if serviceItem.Status.CurrentRevisionID == "" {
		return nodes, false, nil
	}

	// 已有 current revision 时，读取 promoted deployment 判断当前是否有稳定后端。
	activeDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return nil, false, err
	}
	// 只有 running 的 promoted deployment 才参与安全切流固定。
	if activeDeployment == nil || activeDeployment.Status != deploymentmodel.StatusRunning {
		return nodes, false, nil
	}

	// 安全切流只在当前 revision 恰好有一个 running execution 时尝试固定到同一节点。
	activeExecutions, err := s.store.ListRunningExecutionsByDeployment(ctx, activeDeployment.ID)
	if err != nil {
		return nil, false, err
	}
	if len(activeExecutions) != 1 {
		return nodes, false, nil
	}
	activeExecution := activeExecutions[0]

	// 在当前节点列表中查找 active execution 所在节点。
	filtered := make([]node.Node, 0, 1)
	for _, item := range nodes {
		if item.ID == activeExecution.NodeID {
			// 只有该节点剩余容量足够时才固定；容量不足则回退到全量节点继续正常调度。
			cpuFree := item.CPUMilliAllocatable - item.CPUMilliAllocated
			memoryFree := item.MemoryMiAllocatable - item.MemoryMiAllocated
			if cpuFree < cpuMilliRequest || memoryFree < memoryMiRequest {
				return nodes, false, nil
			}
			// 固定成功时只返回 active node，避免候选 revision 被放到其它节点。
			filtered = append(filtered, item)
			break
		}
	}
	// active execution 指向的 node 不在当前候选池时，回退到全量节点。
	if len(filtered) == 0 {
		return nodes, false, nil
	}

	// 返回收窄后的节点列表，并标记 pinnedToActiveNode=true 供失败消息和 scale-out 判断使用。
	return filtered, true, nil
}

// resolvePersistentDirPinnedNode 解析 persistent dir pinned node。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 的持久化记录。
func (s deploymentCoordinator) resolvePersistentDirPinnedNode(ctx context.Context, serviceItem workload.Service) (string, bool, error) {
	// 优先从 promoted deployment 查找 pinned node，因为它代表当前正式承载流量的 revision。
	promotedDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return "", false, err
	}
	// 如果 promoted deployment 有 placement，直接复用它的第一个 nodeID。
	if promotedDeployment != nil {
		nodeID, ok, err := s.resolvePinnedNodeFromDeployment(ctx, promotedDeployment.ID)
		if err != nil || ok {
			return nodeID, ok, err
		}
	}

	// 没有 promoted selection 时，退回 current deployment，覆盖尚未 promoted 但已有 selection 的场景。
	currentDeployment, err := s.store.GetCurrentDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return "", false, err
	}
	// current 与 promoted 不同才继续查，避免重复读取同一个 deployment 的 placement。
	if currentDeployment != nil && (promotedDeployment == nil || currentDeployment.ID != promotedDeployment.ID) {
		return s.resolvePinnedNodeFromDeployment(ctx, currentDeployment.ID)
	}
	// 没有可用 deployment/placement 时表示当前无法解析 pinned node。
	return "", false, nil
}

// resolvePinnedNodeFromDeployment 解析 pinned node from deployment。
// 参数说明：ctx 控制本次请求或后台操作生命周期；deploymentID 是 deployment 唯一标识。
func (s deploymentCoordinator) resolvePinnedNodeFromDeployment(ctx context.Context, deploymentID string) (string, bool, error) {
	// 读取 deployment 已持久化的 selection decisions。
	placements, err := s.store.ListPlacementDecisionsByDeployment(ctx, deploymentID)
	if err != nil {
		return "", false, err
	}
	// 没有 selection 时无法推导 node-local persistent dir 所在节点。
	if len(placements) == 0 {
		return "", false, nil
	}
	// 当前 persistent dir 语义按 deployment 的第一个 selection 固定节点。
	return placements[0].NodeID, true, nil
}

// filterNodesByPinnedNode 过滤 nodes by pinned node。
// 参数说明：nodes 是待调度或待转换的节点列表；nodeID 是 node 唯一标识；cpuMilliRequest 是单副本 CPU 请求，单位 millicore；memoryMiRequest 是单副本内存请求，单位 MiB。
func filterNodesByPinnedNode(nodes []node.Node, nodeID string, cpuMilliRequest int, memoryMiRequest int) []node.Node {
	// pinned node 只可能返回一个候选节点。
	filtered := make([]node.Node, 0, 1)
	for _, item := range nodes {
		// 跳过非目标节点，确保调度不会把 workload 放到其它 node-local 目录。
		if item.ID != nodeID {
			continue
		}
		// 只在 pinned node 剩余资源满足单副本请求时返回该节点。
		cpuFree := item.CPUMilliAllocatable - item.CPUMilliAllocated
		memoryFree := item.MemoryMiAllocatable - item.MemoryMiAllocated
		if cpuFree < cpuMilliRequest || memoryFree < memoryMiRequest {
			return nil
		}
		// 找到目标节点后即可结束；节点 ID 应唯一。
		filtered = append(filtered, item)
		break
	}
	// 未找到目标节点时返回空切片，调用方会据此得到调度失败。
	return filtered
}

// resolveServiceStatusAfterCandidateFailure 解析 service status after candidate failure。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 记录；failedDeploymentID 表示 failed deployment 的唯一标识。
func (s deploymentCoordinator) resolveServiceStatusAfterCandidateFailure(ctx context.Context, serviceItem workload.Service, failedDeploymentID string) (string, string, error) {
	// 没有 current revision 时，候选失败就是首发失败，service 进入 failed。
	if serviceItem.Status.CurrentRevisionID == "" {
		return workload.StatusFailed, "", nil
	}

	// 有 current revision 时，查看 promoted deployment 是否仍能继续承载流量。
	activeDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return "", "", err
	}
	// 如果 promoted deployment 不存在、就是失败 deployment，或不再 running，则 service 视为 failed。
	if activeDeployment == nil || activeDeployment.ID == failedDeploymentID || activeDeployment.Status != deploymentmodel.StatusRunning {
		return workload.StatusFailed, serviceItem.Status.CurrentRevisionID, nil
	}

	// promoted deployment running 时，再检查实际 running execution 数量。
	activeExecutions, err := s.store.ListRunningExecutionsByDeployment(ctx, activeDeployment.ID)
	if err != nil {
		return "", "", err
	}
	// 没有 running execution 表示 current revision 已没有可用后端。
	if len(activeExecutions) == 0 {
		return workload.StatusFailed, serviceItem.Status.CurrentRevisionID, nil
	}
	// running execution 少于 desired replicas 时保留 current revision，但状态降级。
	if len(activeExecutions) < activeDeployment.DesiredReplicas {
		return workload.StatusDegraded, activeDeployment.RevisionID, nil
	}
	// current revision 后端完整运行时，候选失败不影响 service 对外 running 状态。
	return workload.StatusRunning, activeDeployment.RevisionID, nil
}
