package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/cloudplane/infra/store"
)

// loadRuntimeState 加载 service 的当前 revision、候选 revision、deployment 和 execution。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是要读取运行态的 service 记录。
func (o ServiceQueryController) loadRuntimeState(ctx context.Context, serviceItem workload.Service) (RuntimeState, error) {
	// 从空状态开始逐项填充，缺失的关联对象保持 nil。
	state := RuntimeState{}
	// current revision ID 存在时，尝试读取对应 revision 快照。
	if serviceItem.Status.CurrentRevisionID != "" {
		currentRevision, err := o.store.GetRevisionByService(ctx, serviceItem.Metadata.ID, serviceItem.Status.CurrentRevisionID)
		if err != nil && !errors.Is(err, store.ErrRevisionNotFound) {
			return RuntimeState{}, err
		}
		// revision 被删除或缺失时不直接失败，仅在 RuntimeState 中保持 nil。
		if err == nil {
			state.CurrentRevisionID = currentRevision.ID
			state.CurrentRevision = &currentRevision
		}
	}
	// candidate revision 表示正在发布、暂停或失败等待处理的候选版本。
	if serviceItem.Status.CandidateRevisionID != "" {
		candidateRevision, err := o.store.GetRevisionByService(ctx, serviceItem.Metadata.ID, serviceItem.Status.CandidateRevisionID)
		if err != nil && !errors.Is(err, store.ErrRevisionNotFound) {
			return RuntimeState{}, err
		}
		// 候选 revision 缺失时同样保持 nil，不阻断 service 视图构建。
		if err == nil {
			state.CandidateRevision = &candidateRevision
		}
	}

	// currentDeployment 代表当前要展示的 deployment，优先选择正式承载流量的 promoted deployment。
	var currentDeployment *deployment.Deployment
	var err error
	if serviceItem.Status.CurrentRevisionID != "" {
		currentDeployment, err = o.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	} else {
		// 首次发布还没有 current revision 时，退回最新 current deployment。
		currentDeployment, err = o.store.GetCurrentDeploymentByService(ctx, serviceItem.Metadata.ID)
	}
	if err != nil {
		return RuntimeState{}, err
	}
	// 找到 deployment 后继续补充 execution 运行态。
	if currentDeployment != nil {
		state.CurrentDeploymentID = currentDeployment.ID
		state.CurrentDeployment = currentDeployment
		// 优先读取最新 execution，便于展示最近一次上报状态。
		currentExecution, err := o.store.GetLatestExecutionByDeployment(ctx, currentDeployment.ID)
		if err != nil {
			return RuntimeState{}, err
		}
		// 如果最新 execution 不是 running，再扫描 running execution，避免旧失败记录遮住仍在服务的副本。
		if currentExecution == nil || currentExecution.Status != execution.StatusRunning {
			runningExecutions, err := o.store.ListRunningExecutionsByDeployment(ctx, currentDeployment.ID)
			if err != nil {
				return RuntimeState{}, err
			}
			// 当前 RuntimeState 只展示一个 execution，因此取返回列表中的第一个 running execution。
			if len(runningExecutions) > 0 {
				currentExecution = &runningExecutions[0]
			}
		}
		state.CurrentExecution = currentExecution
	}

	// 最后基于 service 持久状态和已加载运行态生成健康摘要。
	state.Healthy, state.Message = summarizeServiceHealth(serviceItem, state)
	return state, nil
}

// summarizeServiceHealth 根据 service 状态和 runtime state 生成健康布尔值与摘要文案。
// 参数说明：serviceItem 是 service 持久化状态；state 是已加载的运行态关联对象。
func summarizeServiceHealth(serviceItem workload.Service, state RuntimeState) (bool, string) {
	// 没有 deployment 时，优先展示 rollout failed 的具体错误。
	if state.CurrentDeployment == nil {
		if serviceItem.Status.RolloutPhase == workload.RolloutPhaseFailed && serviceItem.Status.RolloutMessage != "" {
			return false, serviceItem.Status.RolloutMessage
		}
		// idle service 还没有进入部署流程。
		if serviceItem.Status.Phase == workload.StatusIdle {
			return false, "service exists but no deployment has been created yet"
		}
		// 有 candidate revision 但没有 current deployment，通常表示候选仍在部署早期。
		if serviceItem.Status.CandidateRevisionID != "" {
			return false, "candidate revision is being deployed"
		}
		// 兜底文案覆盖状态不完整或关联记录缺失。
		return false, "no deployment is currently recorded for this service"
	}
	// current revision 的 running execution 与 running deployment 同时存在时，认为当前后端可服务。
	if state.CurrentExecution != nil && state.CurrentExecution.Status == execution.StatusRunning &&
		state.CurrentDeployment.Status == deployment.StatusRunning &&
		serviceItem.Status.CurrentRevisionID != "" &&
		serviceItem.Status.CurrentRevisionID == state.CurrentDeployment.RevisionID {
		// degraded 表示有可服务后端，但 ready 副本数少于期望。
		if serviceItem.Status.Phase == workload.StatusDegraded {
			return false, "current revision is serving traffic with fewer ready replicas than desired"
		}
		// 根据 rollout phase 解释当前后端与候选 revision 的关系。
		switch serviceItem.Status.RolloutPhase {
		case workload.RolloutPhaseProgressing:
			return true, "current revision is serving traffic while the candidate revision is progressing"
		case workload.RolloutPhaseFailed:
			return false, fallbackString(serviceItem.Status.RolloutMessage, "candidate revision failed while the current revision is still serving traffic")
		default:
			return true, "current revision is running"
		}
	}
	// 有 current revision，但当前展示的 deployment 属于 candidate revision，则说明候选尚未提升。
	if serviceItem.Status.CurrentRevisionID != "" && state.CurrentDeployment.RevisionID != serviceItem.Status.CurrentRevisionID {
		// 按 rollout phase 返回候选发布的具体阶段。
		switch serviceItem.Status.RolloutPhase {
		case workload.RolloutPhaseFailed:
			return false, fallbackString(serviceItem.Status.RolloutMessage, "candidate revision failed")
		default:
			return false, "a candidate revision rollout is still in progress"
		}
	}
	// 兜底返回 service/deployment 状态组合，便于排查未覆盖的中间状态。
	return false, fmt.Sprintf("service=%s deployment=%s", serviceItem.Status.Phase, state.CurrentDeployment.Status)
}
