package lifecycle

import (
	"context"
	"fmt"
	"strings"

	"mini-cloud/internal/cloudplane/control/nodepool"
	deploymentmodel "mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/logctx"
)

// schedule 为新 deployment 选择节点，必要时触发 runtime node 扩容，并推进 deployment 状态。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 记录；createdDeployment 是刚创建的 deployment。
func (s deploymentCoordinator) schedule(ctx context.Context, serviceItem workload.Service, createdDeployment deploymentmodel.Deployment) (deploymentmodel.Deployment, workload.Service, *scheduler.StoredDecision, error) {
	// 阶段一：deployment 进入 scheduling，读取 service 所需资源和当前节点列表。
	// 阶段二：persistentDirs 会尽量根据既有 selection 收窄到 node-local 目录所在节点。
	// 阶段三：现有节点无法生成 selection 时才尝试 runtime scale-out；最终失败会写回 deployment/service 状态。
	// 补充 deployment 日志字段，后续调度、扩容和失败落库都能关联同一 deployment。
	ctx = logctx.WithFields(ctx, logctx.Fields{
		ServiceID:    serviceItem.Metadata.ID,
		DeploymentID: createdDeployment.ID,
	})
	// 新建 deployment 先进入 scheduling，表示 cloud-plane 正在为它选择 runtime node。
	currentDeployment, err := s.store.UpdateDeploymentStatus(ctx, createdDeployment.ID, deploymentmodel.StatusScheduling, "asking scheduler to choose a node")
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 根据 instance class 计算单副本资源请求。
	cpuMilliRequest, memoryMiRequest, err := workload.ResourceRequest(serviceItem.Spec.InstanceClass)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 读取当前已注册的 runtime node 作为候选池。
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 对有状态目录或安全切流场景进行候选节点收窄，必要时固定到当前活跃节点。
	nodes, pinnedToActiveNode, err := s.pickCandidateNodes(ctx, serviceItem, nodes, cpuMilliRequest, memoryMiRequest)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 解析本 plane 的 provider 名称，用于本地 scheduler 匹配节点。
	placementProvider, err := s.resolvePlacementProvider()
	if err != nil {
		return s.fail(ctx, serviceItem, currentDeployment, err.Error())
	}

	// 构造 scheduler 输入，要求为本 deployment 的所有副本生成 placement。
	placementRequest := scheduler.PlacementRequest{
		DeploymentID:    createdDeployment.ID,
		Provider:        placementProvider,
		Region:          serviceItem.Spec.Region,
		CPUMilliRequest: cpuMilliRequest,
		MemoryMiRequest: memoryMiRequest,
		Replicas:        serviceItem.Spec.Replicas,
	}

	// 首次只基于当前节点池调度，不立即创建云资源。
	result, err := scheduler.Plan(nodes, placementRequest)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 没有 selection 时再尝试 runtime scale-out；如果被 pinned 到活跃节点，scale-out 会被跳过。
	if len(result.Decisions) == 0 {
		scaleOutResult, err := s.scaleOut.TryForPlacement(ctx, nodepool.ScaleOutRequest{
			Service:            serviceItem,
			Placement:          placementRequest,
			PinnedToActiveNode: pinnedToActiveNode,
			InitialFailure:     result.FailureReason,
		})
		if err != nil {
			return deploymentmodel.Deployment{}, workload.Service{}, nil, err
		}
		// scale-out 返回新节点列表后，再用最新节点池重新调度。
		if len(scaleOutResult.Nodes) > 0 {
			nodes = scaleOutResult.Nodes
			result, err = scheduler.Plan(nodes, placementRequest)
			if err != nil {
				return deploymentmodel.Deployment{}, workload.Service{}, nil, err
			}
		}

		// 如果重新调度仍失败，按 pinned/scale-out 状态生成更准确的用户可读失败原因。
		if len(result.Decisions) == 0 {
			failureReason := result.FailureReason
			if len(serviceItem.Spec.PersistentDirs) > 0 && pinnedToActiveNode {
				failureReason = fmt.Sprintf("persistentDirs are bound to the previously assigned node-local directory, so automatic relocation is blocked: %s", result.FailureReason)
			} else if pinnedToActiveNode {
				failureReason = fmt.Sprintf("safe cutover keeps traffic on the current revision until the candidate is healthy, so the replacement currently has to fit on the active node: %s", result.FailureReason)
			} else if scaleOutResult.Failure != "" {
				failureReason = scaleOutResult.Failure
			} else if scaleOutResult.Summary != "" {
				failureReason = fmt.Sprintf("%s, but scheduling still failed: %s", scaleOutResult.Summary, result.FailureReason)
			}
			return s.fail(ctx, serviceItem, currentDeployment, failureReason)
		}
	}
	// scheduler 返回的 decision 只描述放置结果，写库前统一绑定 deploymentID。
	for i := range result.Decisions {
		result.Decisions[i].DeploymentID = createdDeployment.ID
	}
	// selection 写入、deployment assigned 和 service revision 状态更新必须顺序完成。
	var (
		currentPlacements []scheduler.StoredDecision
		currentService    workload.Service
	)
	// 持久化 scheduler 结果，node-agent 后续 poll work 会消费这些 placement。
	currentPlacements, err = s.store.CreatePlacementDecisions(ctx, placementRequest, result.Decisions)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// selection 写入成功后 deployment 进入 assigned。
	assignReason := fmt.Sprintf("assigned %d replica placement(s)", len(currentPlacements))
	currentDeployment, err = s.store.UpdateDeploymentStatus(ctx, currentDeployment.ID, deploymentmodel.StatusAssigned, assignReason)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 根据本次 launch 的 revision 更新 service current/candidate 状态。
	currentService, err = s.updateServiceRevisionStateAfterLaunch(ctx, serviceItem, createdDeployment.RevisionID)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 理论上 replicas > 0 时应有 placement；这里保留空 selection 兼容当前 store/scheduler 返回形态。
	if len(currentPlacements) == 0 {
		return currentDeployment, currentService, nil, nil
	}
	// API 旧返回只携带一个代表性 placement，因此取第一个持久化结果。
	representativePlacement := currentPlacements[0]
	return currentDeployment, currentService, &representativePlacement, nil
}

// resolvePlacementProvider 解析当前 cloud-plane 参与调度时使用的 provider 名称。
func (s deploymentCoordinator) resolvePlacementProvider() (string, error) {
	if provider := strings.TrimSpace(s.config.Infrastructure.Provider); provider != "" {
		return provider, nil
	}
	return "", fmt.Errorf("placement provider is not configured")
}

// fail 将 deployment 标记失败，并根据当前稳定后端重新计算 service 与 rollout 状态。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 记录；currentDeployment 是当前处理的 deployment；failureReason 记录失败原因。
func (s deploymentCoordinator) fail(ctx context.Context, serviceItem workload.Service, currentDeployment deploymentmodel.Deployment, failureReason string) (deploymentmodel.Deployment, workload.Service, *scheduler.StoredDecision, error) {
	// 先把当前 deployment 标记为 failed，记录调度、扩容或部署推进失败原因。
	currentDeployment, err := s.store.UpdateDeploymentStatus(ctx, currentDeployment.ID, deploymentmodel.StatusFailed, failureReason)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 根据失败的是首个 deployment、候选 deployment 还是 current deployment，计算 service 下一状态。
	nextServiceStatus, promotedRevisionID, err := s.resolveServiceStatusAfterCandidateFailure(ctx, serviceItem, currentDeployment.ID)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}

	// 默认清空候选 rollout，并将 rollout phase 置为 idle；service 状态由 nextServiceStatus 决定。
	nextCandidateRevisionID := ""
	nextRolloutPhase := workload.RolloutPhaseIdle
	nextRolloutMessage := ""
	// 如果失败 deployment 不是当前正式 revision，则它是候选发布失败，需要保留 candidate 供查询/处理。
	isCandidateFailure := serviceItem.Status.CurrentRevisionID == "" || currentDeployment.RevisionID != serviceItem.Status.CurrentRevisionID
	if isCandidateFailure {
		nextCandidateRevisionID = currentDeployment.RevisionID
		nextRolloutPhase = workload.RolloutPhaseFailed
		nextRolloutMessage = failureReason
	}

	// 原子更新 service 的 current/candidate revision、服务状态和 rollout 状态。
	currentService, err := s.store.UpdateServiceRevisionState(
		ctx,
		serviceItem.Metadata.ID,
		promotedRevisionID,
		nextCandidateRevisionID,
		nextServiceStatus,
		nextRolloutPhase,
		nextRolloutMessage,
	)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}
	// fail 路径没有新的 selection 可返回，因此第三个返回值固定为 nil。
	return currentDeployment, currentService, nil, nil
}

// updateServiceRevisionStateAfterLaunch 执行 update service revision state after launch。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 记录；revisionID 是 revision 唯一标识。
func (s deploymentCoordinator) updateServiceRevisionStateAfterLaunch(ctx context.Context, serviceItem workload.Service, revisionID string) (workload.Service, error) {
	// 默认保持原 current revision；新 revision 先作为 candidate 部署。
	nextCurrentRevisionID := serviceItem.Status.CurrentRevisionID
	// 没有 current revision 的首次发布会显示 deploying。
	nextStatus := workload.StatusDeploying

	// revision 与 current 不同，或当前还没有 current revision，都按候选发布处理。
	isCandidateLaunch := nextCurrentRevisionID == "" || revisionID != nextCurrentRevisionID
	if isCandidateLaunch {
		// 如果已有 current revision 继续服务，则 service 对外状态应反映 current 后端的真实情况。
		if nextCurrentRevisionID != "" {
			resolvedStatus, err := s.statusWhileCurrentRevisionServes(ctx, serviceItem)
			if err != nil {
				return workload.Service{}, err
			}
			nextStatus = resolvedStatus
		}
		// 候选 revision 进入 progressing；current revision 保持不变，直到候选副本全部 ready 后由 cloud-plane 自动转正。
		return s.store.UpdateServiceRevisionState(
			ctx,
			serviceItem.Metadata.ID,
			nextCurrentRevisionID,
			revisionID,
			nextStatus,
			workload.RolloutPhaseProgressing,
			"candidate revision is being deployed",
		)
	}

	// revision 等于 current 时，说明是在当前 revision 上重新部署；清空 candidate 并回到 idle。
	return s.store.UpdateServiceRevisionState(
		ctx,
		serviceItem.Metadata.ID,
		nextCurrentRevisionID,
		"",
		nextStatus,
		workload.RolloutPhaseIdle,
		"",
	)
}

// statusWhileCurrentRevisionServes 执行 status while current revision serves。
// 参数说明：ctx 控制本次请求或后台操作生命周期；serviceItem 是目标 service 的持久化记录。
func (s deploymentCoordinator) statusWhileCurrentRevisionServes(ctx context.Context, serviceItem workload.Service) (string, error) {
	// 没有 current revision 时只能报告 deploying，等待首个 revision 完成运行态反馈。
	if serviceItem.Status.CurrentRevisionID == "" {
		return workload.StatusDeploying, nil
	}

	// current revision 存在时，读取 promoted deployment 作为当前承载流量的 deployment。
	currentDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return "", err
	}
	// 没有 running promoted deployment 时，沿用 service 当前状态，不在这里强行降级。
	if currentDeployment == nil || currentDeployment.Status != deploymentmodel.StatusRunning {
		return serviceItem.Status.Phase, nil
	}

	// 读取 running execution 数量，用于判断当前 revision 是否完整承载 desired replicas。
	runningExecutions, err := s.store.ListRunningExecutionsByDeployment(ctx, currentDeployment.ID)
	if err != nil {
		return "", err
	}
	// 没有 running execution 时同样沿用当前 service 状态，让执行上报路径负责状态推进。
	if len(runningExecutions) == 0 {
		return serviceItem.Status.Phase, nil
	}
	// running execution 少于 desired replicas 时，当前 revision 能服务但容量不完整。
	if len(runningExecutions) < currentDeployment.DesiredReplicas {
		return workload.StatusDegraded, nil
	}
	// running execution 达到 desired replicas，认为 current revision 当前处于 running。
	return workload.StatusRunning, nil
}
