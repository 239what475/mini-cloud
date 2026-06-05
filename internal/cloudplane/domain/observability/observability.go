// Package observability 定义 cloud-plane 概览、可靠性输入信号和告警快照。
package observability

import (
	"fmt"
	"time"
)

const (
	// ExecutionPlanStuckThresholdSeconds 是判定 execution plan 长时间卡在非终态的默认阈值。
	ExecutionPlanStuckThresholdSeconds = int64((10 * time.Minute) / time.Second)
	// RuntimeNodeRegistrationTimeoutSeconds 是判定 runtime node bootstrap 超时的默认阈值。
	RuntimeNodeRegistrationTimeoutSeconds = int64((10 * time.Minute) / time.Second)
)

// Overview 汇总 cloud-plane 当前资源数量和状态分布。
type Overview struct {
	// ServicesTotal 是当前 service 总数。
	ServicesTotal int `json:"servicesTotal"`
	// ServicesIdle 是处于 idle 状态的 service 数量。
	ServicesIdle int `json:"servicesIdle"`
	// ServicesDeploying 是正在发布或变更的 service 数量。
	ServicesDeploying int `json:"servicesDeploying"`
	// ServicesRunning 是当前稳定运行的 service 数量。
	ServicesRunning int `json:"servicesRunning"`
	// ServicesDegraded 是当前处于降级状态的 service 数量。
	ServicesDegraded int `json:"servicesDegraded"`
	// ServicesFailed 是当前发布或运行失败的 service 数量。
	ServicesFailed int `json:"servicesFailed"`

	// NodesTotal 是当前 node 总数。
	NodesTotal int `json:"nodesTotal"`
	// NodesRegistering 是正在注册但尚未 ready 的 node 数量。
	NodesRegistering int `json:"nodesRegistering"`
	// NodesReady 是状态为 ready 的 node 数量。
	NodesReady int `json:"nodesReady"`
	// NodesNotReady 是已注册但暂不可用的 node 数量。
	NodesNotReady int `json:"nodesNotReady"`
	// NodesDraining 是正在排空、不再接收新调度的 node 数量。
	NodesDraining int `json:"nodesDraining"`
	// NodesOffline 是心跳超时或被判定离线的 node 数量。
	NodesOffline int `json:"nodesOffline"`

	// ExecutionPlansTotal 是当前 execution plan 总数。
	ExecutionPlansTotal int `json:"executionPlansTotal"`
	// ExecutionPlansPending 是等待进入调度流程的 execution plan 数量。
	ExecutionPlansPending int `json:"executionPlansPending"`
	// ExecutionPlansScheduling 是正在选择运行节点的 execution plan 数量。
	ExecutionPlansScheduling int `json:"executionPlansScheduling"`
	// ExecutionPlansAssigned 是已经分配节点但执行尚未开始或尚未完成的 execution plan 数量。
	ExecutionPlansAssigned int `json:"executionPlansAssigned"`
	// ExecutionPlansDeploying 是 node-agent 正在执行的 execution plan 数量。
	ExecutionPlansDeploying int `json:"executionPlansDeploying"`
	// ExecutionPlansRunning 是已成功进入 running 的 execution plan 数量。
	ExecutionPlansRunning int `json:"executionPlansRunning"`
	// ExecutionPlansFailed 是终态为 failed 的 execution plan 数量。
	ExecutionPlansFailed int `json:"executionPlansFailed"`

	// ExecutionsTotal 是当前 execution intent 总数。
	ExecutionsTotal int `json:"executionsTotal"`
	// ExecutionsDeploying 是仍在执行中的 execution intent 数量。
	ExecutionsDeploying int `json:"executionsDeploying"`
	// ExecutionsRunning 是已成功运行的 execution intent 数量。
	ExecutionsRunning int `json:"executionsRunning"`
	// ExecutionsFailed 是执行失败的 execution intent 数量。
	ExecutionsFailed int `json:"executionsFailed"`
}

// ExecutionPlanStuckSignal 汇总超过阈值仍未进入终态的 execution plan。
type ExecutionPlanStuckSignal struct {
	// ThresholdSeconds 是本次统计使用的卡住判定阈值秒数。
	ThresholdSeconds int64 `json:"thresholdSeconds"`
	// Total 是该信号覆盖的对象总数。
	Total int `json:"total"`
	// Pending 是卡在 pending 状态的 execution plan 数量。
	Pending int `json:"pending"`
	// Scheduling 是卡在 scheduling 状态的 execution plan 数量。
	Scheduling int `json:"scheduling"`
	// Assigned 是卡在 assigned 状态的 execution plan 数量。
	Assigned int `json:"assigned"`
	// Deploying 是卡在 deploying 状态的 execution plan 数量。
	Deploying int `json:"deploying"`
	// OldestAgeSeconds 是最早一条非终态 execution plan 已持续的秒数；Total>0 时可用于描述卡住对象的最长持续时间。
	OldestAgeSeconds int64 `json:"oldestAgeSeconds"`
}

// RuntimeNodeRegistrationSignal 汇总 runtime node 创建、注册和 ready 阶段的状态。
type RuntimeNodeRegistrationSignal struct {
	// ThresholdSeconds 是本次统计使用的卡住判定阈值秒数。
	ThresholdSeconds int64 `json:"thresholdSeconds"`
	// Total 是纳入 bootstrap 阶段统计的 runtime node 总数。
	Total int `json:"total"`
	// Provisioning 是仍处于云资源创建或 agent bootstrap 阶段的 runtime node 数量。
	Provisioning int `json:"provisioning"`
	// Ready 是状态为 ready 的 runtime node 数量。
	Ready int `json:"ready"`
	// ProvisioningTimedOut 是超过 bootstrap 阈值仍未 ready 的 runtime node 数量。
	ProvisioningTimedOut int `json:"provisioningTimedOut"`
	// OldestProvisioningAgeSeconds 是最早一个 provisioning runtime node 已持续的秒数。
	OldestProvisioningAgeSeconds int64 `json:"oldestProvisioningAgeSeconds"`
}

// ExecutionPlanRolloutCounterSignal 汇总 execution plan 首次终态结果。
type ExecutionPlanRolloutCounterSignal struct {
	// Total 是已记录首次终态结果的 execution plan 总数。
	Total int64 `json:"total"`
	// Success 是首次终态为 running 的 execution plan 数量。
	Success int64 `json:"success"`
	// Failed 是首次终态为 failed 的 execution plan 数量。
	Failed int64 `json:"failed"`
}

// RuntimeNodeBootstrapCounterSignal 汇总 runtime node bootstrap 尝试和成功次数。
type RuntimeNodeBootstrapCounterSignal struct {
	// Started 是 cloud-plane 记录的 runtime node bootstrap 开始次数。
	Started int64 `json:"started"`
	// Ready 是 bootstrap 尝试中最终上报 ready 的累计次数。
	Ready int64 `json:"ready"`
}

// ReliabilityInputs 是构建可靠性快照所需的原始聚合输入。
type ReliabilityInputs struct {
	// Overview 是平台资源数量和状态概览。
	Overview Overview
	// RuntimeNodesTotal 是当前 runtime node 总数。
	RuntimeNodesTotal int
	// RuntimeNodesReady 是当前 ready 的 runtime node 数量。
	RuntimeNodesReady int
	// RuntimeNodesOffline 是当前离线的 runtime node 数量。
	RuntimeNodesOffline int
	// RuntimeNodesNotReady 是当前未 ready 且尚未离线的 runtime node 数量。
	RuntimeNodesNotReady int
	// RuntimeNodesDraining 是当前正在排空的 runtime node 数量。
	RuntimeNodesDraining int
	// ExecutionPlanStuck 是 execution plan 卡住状态聚合。
	ExecutionPlanStuck ExecutionPlanStuckSignal
	// RuntimeNodeRegistration 是 runtime node 注册和 bootstrap 状态聚合。
	RuntimeNodeRegistration RuntimeNodeRegistrationSignal
	// ExecutionPlanRolloutCounters 是 execution plan 结果累计计数器。
	ExecutionPlanRolloutCounters ExecutionPlanRolloutCounterSignal
	// RuntimeNodeBootstrapCounts 是 runtime node bootstrap 累计计数器。
	RuntimeNodeBootstrapCounts RuntimeNodeBootstrapCounterSignal
}

// ReliabilitySnapshot 是面向内部 gRPC 同步和控制面聚合的告警视图。
type ReliabilitySnapshot struct {
	// GeneratedAt 是可靠性快照生成时间。
	GeneratedAt time.Time `json:"generatedAt"`
	// Alerts 是本次快照计算出的告警状态。
	Alerts []AlertStatus `json:"alerts"`
}

// AlertStatus 描述一个内置平台告警在当前快照中的状态。
type AlertStatus struct {
	// ID 是内置告警规则的稳定标识。
	ID string `json:"id"`
	// Name 是展示给操作者的告警名称。
	Name string `json:"name"`
	// Severity 是告警严重级别。
	Severity string `json:"severity"`
	// State 是告警当前状态。
	State string `json:"state"`
	// Summary 是给用户展示的状态摘要。
	Summary string `json:"summary"`
	// Observed 是触发判断使用的当前观测值。
	Observed float64 `json:"observed"`
	// Threshold 是触发判断使用的阈值。
	Threshold float64 `json:"threshold"`
}

// BuildReliabilitySnapshot 根据平台聚合输入计算告警状态。
// 参数说明：now 是快照生成时间；input 是 store 聚合出的可靠性计算输入。
func BuildReliabilitySnapshot(now time.Time, input ReliabilityInputs) ReliabilitySnapshot {
	alerts := []AlertStatus{
		buildExecutionPlanFailureAlert(input),
		buildExecutionPlanStuckAlert(input),
		buildOfflineRuntimeNodeAlert(input),
		buildRuntimeNodeBootstrapStuckAlert(input),
	}
	return ReliabilitySnapshot{
		GeneratedAt: now,
		Alerts:      alerts,
	}
}

// buildExecutionPlanFailureAlert 根据当前 failed execution plan 数量生成发布失败告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildExecutionPlanFailureAlert(input ReliabilityInputs) AlertStatus {
	// 当前失败 execution plan 数量来自 overview 聚合。
	failed := input.Overview.ExecutionPlansFailed
	// 默认告警为 ok；存在任意 failed execution plan 时触发 warning。
	state := "ok"
	summary := "当前没有失败 execution plan"
	if failed > 0 {
		state = "firing"
		summary = "存在失败 execution plan，需要检查最近 service 更新、重试或发布控制动作"
	}
	// Threshold=0 表示只要 observed 大于 0 即触发。
	return AlertStatus{
		ID:        "execution-plan-failures-active",
		Name:      "发布失败告警",
		Severity:  "warning",
		State:     state,
		Summary:   summary,
		Observed:  float64(failed),
		Threshold: 0,
	}
}

// buildOfflineRuntimeNodeAlert 根据离线 runtime node 数量生成节点离线告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildOfflineRuntimeNodeAlert(input ReliabilityInputs) AlertStatus {
	// 默认没有离线 runtime node。
	state := "ok"
	summary := "当前没有离线 runtime node"
	// 任意 runtime node offline 都触发 critical 告警。
	if input.RuntimeNodesOffline > 0 {
		state = "firing"
		summary = "存在离线 runtime node，需要检查节点、心跳和恢复流程"
	}
	// Observed 保存当前 offline runtime node 数量。
	return AlertStatus{
		ID:        "runtime-nodes-offline",
		Name:      "runtime node 离线告警",
		Severity:  "critical",
		State:     state,
		Summary:   summary,
		Observed:  float64(input.RuntimeNodesOffline),
		Threshold: 0,
	}
}

// buildExecutionPlanStuckAlert 根据卡住 execution plan 聚合生成发布卡住告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildExecutionPlanStuckAlert(input ReliabilityInputs) AlertStatus {
	// 默认没有超过阈值仍卡在非终态的 execution plan。
	state := "ok"
	summary := "当前没有长时间卡住的 execution plan。"
	// Total>0 表示至少一个 execution plan 超过卡住阈值。
	if input.ExecutionPlanStuck.Total > 0 {
		state = "firing"
		// 告警摘要携带卡住数量、阈值和最老持续时间，方便直接定位严重度。
		summary = fmt.Sprintf(
			"存在 %d 个 execution plan 超过 %d 秒仍停留在非终态，最老一条已持续 %d 秒。",
			input.ExecutionPlanStuck.Total,
			input.ExecutionPlanStuck.ThresholdSeconds,
			input.ExecutionPlanStuck.OldestAgeSeconds,
		)
	}
	// Observed 保存当前卡住 execution plan 数量。
	return AlertStatus{
		ID:        "execution-plan-stuck",
		Name:      "发布长时间卡住告警",
		Severity:  "critical",
		State:     state,
		Summary:   summary,
		Observed:  float64(input.ExecutionPlanStuck.Total),
		Threshold: 0,
	}
}

// buildRuntimeNodeBootstrapStuckAlert 根据 bootstrap 超时节点数量生成告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildRuntimeNodeBootstrapStuckAlert(input ReliabilityInputs) AlertStatus {
	// 默认没有 runtime node bootstrap 超时。
	state := "ok"
	summary := "当前没有长时间未完成 bootstrap 的 runtime node。"
	// ProvisioningTimedOut>0 表示存在超过阈值仍未 ready 的 runtime node。
	if input.RuntimeNodeRegistration.ProvisioningTimedOut > 0 {
		state = "firing"
		// 摘要携带超时数量、阈值和最老持续时间。
		summary = fmt.Sprintf(
			"存在 %d 个 runtime node 超过 %d 秒仍停留在 provisioning，说明 bootstrap 或 ready 链路卡住，最老一条已持续 %d 秒。",
			input.RuntimeNodeRegistration.ProvisioningTimedOut,
			input.RuntimeNodeRegistration.ThresholdSeconds,
			input.RuntimeNodeRegistration.OldestProvisioningAgeSeconds,
		)
	}
	// Observed 保存 bootstrap 超时 runtime node 数量。
	return AlertStatus{
		ID:        "runtime-node-bootstrap-stuck",
		Name:      "runtime node bootstrap 卡住告警",
		Severity:  "critical",
		State:     state,
		Summary:   summary,
		Observed:  float64(input.RuntimeNodeRegistration.ProvisioningTimedOut),
		Threshold: 0,
	}
}
