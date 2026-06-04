// Package observability 定义 cloud-plane 概览、可靠性快照、SLO、告警和 runbook 派生视图。
package observability

import (
	"fmt"
	"time"
)

const (
	// DeploymentStuckThresholdSeconds 是判定 deployment 长时间卡在非终态的默认阈值。
	DeploymentStuckThresholdSeconds = int64((10 * time.Minute) / time.Second)
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

	// DeploymentsTotal 是当前 deployment 总数。
	DeploymentsTotal int `json:"deploymentsTotal"`
	// DeploymentsPending 是等待进入调度流程的 deployment 数量。
	DeploymentsPending int `json:"deploymentsPending"`
	// DeploymentsScheduling 是正在选择运行节点的 deployment 数量。
	DeploymentsScheduling int `json:"deploymentsScheduling"`
	// DeploymentsAssigned 是已经分配节点但执行尚未开始或尚未完成的 deployment 数量。
	DeploymentsAssigned int `json:"deploymentsAssigned"`
	// DeploymentsDeploying 是 node-agent 正在部署的 deployment 数量。
	DeploymentsDeploying int `json:"deploymentsDeploying"`
	// DeploymentsRunning 是已成功进入 running 的 deployment 数量。
	DeploymentsRunning int `json:"deploymentsRunning"`
	// DeploymentsFailed 是终态为 failed 的 deployment 数量。
	DeploymentsFailed int `json:"deploymentsFailed"`

	// ExecutionsTotal 是当前 deployment execution 总数。
	ExecutionsTotal int `json:"executionsTotal"`
	// ExecutionsDeploying 是仍在执行中的 deployment execution 数量。
	ExecutionsDeploying int `json:"executionsDeploying"`
	// ExecutionsRunning 是已成功运行的 deployment execution 数量。
	ExecutionsRunning int `json:"executionsRunning"`
	// ExecutionsFailed 是执行失败的 deployment execution 数量。
	ExecutionsFailed int `json:"executionsFailed"`
}

// DeploymentStuckSignal 汇总超过阈值仍未进入终态的 deployment。
type DeploymentStuckSignal struct {
	// ThresholdSeconds 是本次统计使用的卡住判定阈值秒数。
	ThresholdSeconds int64 `json:"thresholdSeconds"`
	// Total 是该信号覆盖的对象总数。
	Total int `json:"total"`
	// Pending 是卡在 pending 状态的 deployment 数量。
	Pending int `json:"pending"`
	// Scheduling 是卡在 scheduling 状态的 deployment 数量。
	Scheduling int `json:"scheduling"`
	// Assigned 是卡在 assigned 状态的 deployment 数量。
	Assigned int `json:"assigned"`
	// Deploying 是卡在 deploying 状态的 deployment 数量。
	Deploying int `json:"deploying"`
	// OldestAgeSeconds 是最早一条非终态 deployment 已持续的秒数；Total>0 时可用于描述卡住对象的最长持续时间。
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

// DeploymentRolloutCounterSignal 汇总 deployment 首次终态结果。
type DeploymentRolloutCounterSignal struct {
	// Total 是已记录首次终态结果的 deployment 总数。
	Total int64 `json:"total"`
	// Success 是首次终态为 running 的 deployment 数量。
	Success int64 `json:"success"`
	// Failed 是首次终态为 failed 的 deployment 数量。
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
	// TerminalDeploymentsLast24h 是创建于最近 24 小时且当前状态为 running/failed 的 deployment 数量。
	TerminalDeploymentsLast24h int
	// SuccessfulDeploymentsLast24h 是创建于最近 24 小时且当前状态为 running 的 deployment 数量。
	SuccessfulDeploymentsLast24h int
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
	// DeploymentStuck 是 deployment 卡住状态聚合。
	DeploymentStuck DeploymentStuckSignal
	// RuntimeNodeRegistration 是 runtime node 注册和 bootstrap 状态聚合。
	RuntimeNodeRegistration RuntimeNodeRegistrationSignal
	// DeploymentRolloutCounters 是 deployment rollout 结果累计计数器。
	DeploymentRolloutCounters DeploymentRolloutCounterSignal
	// RuntimeNodeBootstrapCounts 是 runtime node bootstrap 累计计数器。
	RuntimeNodeBootstrapCounts RuntimeNodeBootstrapCounterSignal
}

// ReliabilitySnapshot 是面向内部 gRPC 同步和控制面聚合的可靠性派生视图。
type ReliabilitySnapshot struct {
	// GeneratedAt 是可靠性快照生成时间。
	GeneratedAt time.Time `json:"generatedAt"`
	// SLOs 是本次快照计算出的服务目标状态。
	SLOs []SLOStatus `json:"slos"`
	// Alerts 是本次快照计算出的告警状态。
	Alerts []AlertStatus `json:"alerts"`
	// Runbooks 是告警关联的内置排障步骤。
	Runbooks []Runbook `json:"runbooks"`
	// DailyChecks 是日常巡检建议项。
	DailyChecks []DailyCheck `json:"dailyChecks"`
}

// SLOStatus 描述一个平台级 SLO 在当前统计窗口内的计算结果。
type SLOStatus struct {
	// ID 是内置 SLO 的稳定标识。
	ID string `json:"id"`
	// Name 是展示给操作者的 SLO 名称。
	Name string `json:"name"`
	// Window 是 SLO 统计窗口。
	Window string `json:"window"`
	// Objective 是该 SLO 的中文目标描述。
	Objective string `json:"objective"`
	// Metric 描述该 SLO 使用的事件口径。
	Metric string `json:"metric"`
	// TargetRatio 是目标达成比例。
	TargetRatio float64 `json:"targetRatio"`
	// ObservedRatio 是当前窗口内观测到的达成比例；no_data 时使用 1 作为展示占位值。
	ObservedRatio float64 `json:"observedRatio"`
	// GoodEvents 是满足 SLO 条件的事件数。
	GoodEvents int `json:"goodEvents"`
	// TotalEvents 是当前 SLO 统计窗口内参与计算的事件总数。
	TotalEvents int `json:"totalEvents"`
	// Status 是该 SLO 当前健康状态。
	Status string `json:"status"`
	// Summary 是给用户展示的状态摘要。
	Summary string `json:"summary"`
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
	// RunbookID 表示 runbook 的唯一标识。
	RunbookID string `json:"runbookID"`
}

// Runbook 描述某类平台故障的排查、处理和验证步骤。
type Runbook struct {
	// ID 是内置 runbook 的稳定标识。
	ID string `json:"id"`
	// Title 是展示给操作者的标题。
	Title string `json:"title"`
	// Summary 是给用户展示的状态摘要。
	Summary string `json:"summary"`
	// AppliesTo 是该 runbook 覆盖的告警 ID 列表。
	AppliesTo []string `json:"appliesTo"`
	// Checks 是处理前建议先确认的事实。
	Checks []string `json:"checks"`
	// Actions 是确认故障后可执行的修复动作。
	Actions []string `json:"actions"`
	// Verification 是修复后需要验证的恢复条件。
	Verification []string `json:"verification"`
}

// DailyCheck 表示一个可直接展示在平台概览中的巡检项。
type DailyCheck struct {
	// ID 是巡检项的稳定标识。
	ID string `json:"id"`
	// Title 是展示给操作者的标题。
	Title string `json:"title"`
	// Status 是该巡检项当前健康状态。
	Status string `json:"status"`
	// Summary 是给用户展示的状态摘要。
	Summary string `json:"summary"`
	// RunbookID 表示 runbook 的唯一标识。
	RunbookID string `json:"runbookID,omitempty"`
}

// BuildReliabilitySnapshot 根据平台聚合输入计算 SLO、告警、runbook 和巡检结果。
// 参数说明：now 是快照生成时间；input 是 store 聚合出的可靠性计算输入。
func BuildReliabilitySnapshot(now time.Time, input ReliabilityInputs) ReliabilitySnapshot {
	// 先计算平台当前内置的 SLO 状态。
	slos := []SLOStatus{
		buildRevisionSLO(input),
		buildRuntimeNodeReadinessSLO(input),
	}
	// 再计算内置告警状态；告警只依赖 input 中的聚合计数和信号。
	alerts := []AlertStatus{
		buildRevisionFailureAlert(input),
		buildDeploymentStuckAlert(input),
		buildOfflineRuntimeNodeAlert(input),
		buildRuntimeNodeBootstrapStuckAlert(input),
	}

	// 统计 firing 告警数量，用于生成日常巡检摘要。
	firingAlerts := 0
	for _, alert := range alerts {
		if alert.State == "firing" {
			firingAlerts++
		}
	}

	// 组合最终快照；runbook 是静态内置列表，daily checks 由 SLO、告警和输入聚合信号派生。
	return ReliabilitySnapshot{
		GeneratedAt: now,
		SLOs:        slos,
		Alerts:      alerts,
		Runbooks:    defaultRunbooks(),
		DailyChecks: []DailyCheck{
			buildAlertDailyCheck(firingAlerts),
			buildRuntimeNodeDailyCheck(input),
			buildDeliveryDailyCheck(slos[0]),
		},
	}
}

// buildRevisionSLO 计算创建于最近 24 小时的 deployment 当前成功率 SLO。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildRevisionSLO(input ReliabilityInputs) SLOStatus {
	// revision rollout SLO 复用通用比例 SLO 构造器。
	return buildRatioSLO(
		// id 作为机器可读稳定标识，供 UI 或文档引用。
		"revision-rollout-success-24h",
		"发布成功率",
		"24h",
		// objective 和 metric 是给使用者看的目标说明和统计口径说明。
		"创建于最近 24 小时的 deployment 当前 running 比例不低于 95%",
		"deployments created in the last 24h whose current status is running / running or failed",
		0.95,
		// good/total 来自 store 预聚合结果；没有样本时由 buildRatioSLO 处理为 no_data。
		input.SuccessfulDeploymentsLast24h,
		input.TerminalDeploymentsLast24h,
	)
}

// buildRuntimeNodeReadinessSLO 计算当前 runtime node 就绪率 SLO。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildRuntimeNodeReadinessSLO(input ReliabilityInputs) SLOStatus {
	// runtime node readiness SLO 复用通用比例 SLO 构造器。
	return buildRatioSLO(
		// id 作为机器可读稳定标识，供 UI 或文档引用。
		"runtime-node-readiness",
		"runtime node 就绪率",
		"current",
		// objective 和 metric 描述当前时刻 runtime node ready/total 统计口径。
		"当前 runtime node 就绪率不低于 95%",
		"ready runtime nodes / total runtime nodes",
		0.95,
		// good/total 来自 store 预聚合结果；没有 runtime node 时由 buildRatioSLO 处理为 no_data。
		input.RuntimeNodesReady,
		input.RuntimeNodesTotal,
	)
}

// buildRatioSLO 按 good/total 样本数生成通用比例型 SLO 状态。
// 参数说明：id 是 SLO 唯一标识；name 是展示名；window 是统计窗口；objective 是目标描述；metric 是统计口径说明；target 是目标比例；good 和 total 是达标样本数与总样本数。
func buildRatioSLO(id string, name string, window string, objective string, metric string, target float64, good int, total int) SLOStatus {
	// total<=0 表示当前窗口没有样本；不把 no data 当成 breached。
	if total <= 0 {
		return SLOStatus{
			ID:            id,
			Name:          name,
			Window:        window,
			Objective:     objective,
			Metric:        metric,
			TargetRatio:   target,
			ObservedRatio: 1,
			GoodEvents:    good,
			TotalEvents:   total,
			Status:        "no_data",
			Summary:       "当前窗口内还没有足够样本",
		}
	}

	// observed ratio 是达标样本数除以总样本数。
	ratio := float64(good) / float64(total)
	// 默认认为达标；低于目标阈值时才标记 breached。
	status := "ok"
	summary := "当前已经满足目标"
	if ratio < target {
		status = "breached"
		summary = "当前已经低于目标阈值"
	}

	// 返回完整 SLO 状态，包含目标值、观测值和事件计数。
	return SLOStatus{
		ID:            id,
		Name:          name,
		Window:        window,
		Objective:     objective,
		Metric:        metric,
		TargetRatio:   target,
		ObservedRatio: ratio,
		GoodEvents:    good,
		TotalEvents:   total,
		Status:        status,
		Summary:       summary,
	}
}

// buildRevisionFailureAlert 根据当前 failed deployment 数量生成发布失败告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildRevisionFailureAlert(input ReliabilityInputs) AlertStatus {
	// 当前失败 deployment 数量来自 overview 聚合。
	failed := input.Overview.DeploymentsFailed
	// 默认告警为 ok；存在任意 failed deployment 时触发 warning。
	state := "ok"
	summary := "当前没有失败 deployment"
	if failed > 0 {
		state = "firing"
		summary = "存在失败 deployment，需要检查最近 service 更新、重试或发布控制动作"
	}
	// Threshold=0 表示只要 observed 大于 0 即触发。
	return AlertStatus{
		ID:        "deployment-failures-active",
		Name:      "发布失败告警",
		Severity:  "warning",
		State:     state,
		Summary:   summary,
		Observed:  float64(failed),
		Threshold: 0,
		RunbookID: "deployment-failed",
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
		RunbookID: "runtime-node-offline",
	}
}

// buildDeploymentStuckAlert 根据卡住 deployment 聚合生成发布卡住告警。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildDeploymentStuckAlert(input ReliabilityInputs) AlertStatus {
	// 默认没有超过阈值仍卡在非终态的 deployment。
	state := "ok"
	summary := "当前没有长时间卡住的 deployment。"
	// Total>0 表示至少一个 deployment 超过卡住阈值。
	if input.DeploymentStuck.Total > 0 {
		state = "firing"
		// 告警摘要携带卡住数量、阈值和最老持续时间，方便直接定位严重度。
		summary = fmt.Sprintf(
			"存在 %d 个 deployment 超过 %d 秒仍停留在非终态，最老一条已持续 %d 秒。",
			input.DeploymentStuck.Total,
			input.DeploymentStuck.ThresholdSeconds,
			input.DeploymentStuck.OldestAgeSeconds,
		)
	}
	// Observed 保存当前卡住 deployment 数量。
	return AlertStatus{
		ID:        "deployment-rollout-stuck",
		Name:      "发布长时间卡住告警",
		Severity:  "critical",
		State:     state,
		Summary:   summary,
		Observed:  float64(input.DeploymentStuck.Total),
		Threshold: 0,
		RunbookID: "deployment-stuck",
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
		RunbookID: "runtime-node-bootstrap-stuck",
	}
}

// defaultRunbooks 返回平台内置的故障排查 runbook 列表。
func defaultRunbooks() []Runbook {
	// runbook 是静态知识库，不依赖当前输入；告警通过 RunbookID/AppliesTo 关联。
	return []Runbook{
		{
			// deployment-failed 处理已经进入 failed 终态的发布。
			ID:        "deployment-failed",
			Title:     "发布失败排查",
			Summary:   "当 deployment 进入 failed，优先确认是调度失败、镜像拉取失败，还是新 revision 健康检查没通过。",
			AppliesTo: []string{"deployment-failures-active"},
			Checks: []string{
				"通过 control-plane 查看 deployment 列表，确认失败 deployment 的 service、revision 和状态原因。",
				"通过 control-plane 查看项目操作记录，确认是不是刚发生了 service update、retry 或发布控制动作。",
				"如果 service 已有旧稳定版本，确认当前 safe deployment 是否仍保持旧流量。",
			},
			Actions: []string{
				"通过 control-plane 对失败 service 执行主动探测，确认当前 backend 是否仍可访问。",
				"如果只是瞬时问题，可通过 control-plane 触发 retry。",
				"如果新 revision 本身有问题，修正 service spec 后重新发布；已有旧稳定版本时 cloud-plane 会继续保留旧流量。",
			},
			Verification: []string{
				"确认 deployment 不再处于 failed。",
				"确认 service.status.currentRevision.id 和当前路由已经恢复到预期状态。",
			},
		},
		{
			// deployment-stuck 处理长时间停留在 pending/scheduling/assigned/deploying 的发布。
			ID:        "deployment-stuck",
			Title:     "发布长时间卡住排查",
			Summary:   "当 deployment 长时间停留在 pending、scheduling、assigned 或 deploying，先判断是调度卡住，还是后续执行没有完成。",
			AppliesTo: []string{"deployment-rollout-stuck"},
			Checks: []string{
				"通过 control-plane 查看 deployment，确认卡住 deployment 当前停在哪个状态，以及最后更新时间。",
				"通过 control-plane 查看 placement，确认是不是没有合适节点，还是已经分配到节点但执行没有推进。",
				"如果已经分配到节点，继续查看对应 execution、node 和日志，确认是 agent 没拉起还是健康检查没完成。",
			},
			Actions: []string{
				"如果是节点容量或状态问题，先处理 runtime node 可用性或重新调度。",
				"如果是某次部署过程卡死，可按 service 维度执行 retry，或通过 control-plane 停止当前候选发布。",
				"如果是镜像或配置错误导致，先修正 revision 对应的规格再重新发布。",
			},
			Verification: []string{
				"确认 deployment 不再长期停留在非终态。",
				"确认对应 service 的当前 revision 已进入 running 或明确 failed。",
			},
		},
		{
			// runtime-node-offline 处理已经注册但心跳超时或被判定离线的 runtime node。
			ID:        "runtime-node-offline",
			Title:     "runtime node 离线排查",
			Summary:   "当 runtime node 离线时，先判断是 node-agent 心跳断了，还是节点本身已经不可达。",
			AppliesTo: []string{"runtime-nodes-offline"},
			Checks: []string{
				"通过 control-plane 查看节点列表，确认哪些节点进入 offline。",
				"查看该节点最近操作历史和心跳时间，判断是短暂抖动还是持续失联。",
				"确认 cloud-plane 到 runtime node 的网络、node-agent 进程和宿主机状态。",
			},
			Actions: []string{
				"如果节点已经恢复，重新让 agent 上报 heartbeat。",
				"如果节点确认可用但还没恢复调度，可通过 control-plane 激活节点。",
				"如果节点仍需维护，保持 draining 或替换该 runtime node。",
			},
			Verification: []string{
				"确认该 runtime node 从 offline 回到 ready。",
				"确认 runtime node readiness SLO 已恢复。",
			},
		},
		{
			// runtime-node-bootstrap-stuck 处理 provider 已开始创建但 node-agent 长时间未 ready 的节点。
			ID:        "runtime-node-bootstrap-stuck",
			Title:     "runtime node bootstrap 卡住排查",
			Summary:   "当 runtime node 已经被 provision 出来，但长时间还停留在 provisioning，先区分是实例没起来、agent 没启动，还是 ready 链路没有完成。",
			AppliesTo: []string{"runtime-node-bootstrap-stuck"},
			Checks: []string{
				"通过 control-plane 查看 runtime node，确认哪些节点仍停留在 provisioning，以及 provisionedAt 时间。",
				"对照对应 deployment 和实例信息，确认云侧实例是否真的已经创建成功。",
				"检查该实例上的 agent 进程、bootstrap 日志，以及它到 cloud-plane 的注册、心跳和 ready 链路。",
			},
			Actions: []string{
				"如果实例启动失败，先人工处理云侧实例，并按当前运行手册清理本地记录或重建 runtime node。",
				"如果 agent 没拉起，修复 bootstrap、二进制分发或启动命令后重试。",
				"如果 ready 链路异常，先修正 cloud-plane 地址、token、健康检查或网络放通配置。",
			},
			Verification: []string{
				"确认 runtime node 从 provisioning 进入 ready，且已关联 nodeID。",
				"确认对应 deployment 可以继续推进到 running。",
			},
		},
	}
}

// buildAlertDailyCheck 生成“当前是否存在 firing 告警”的巡检项。
// 参数说明：firingAlerts 是当前处于 firing 状态的告警数量。
func buildAlertDailyCheck(firingAlerts int) DailyCheck {
	// 没有 firing 告警时，巡检项直接为 ok。
	if firingAlerts == 0 {
		return DailyCheck{
			ID:      "alerts-clear",
			Title:   "检查当前是否存在 firing 告警",
			Status:  "ok",
			Summary: "当前没有 firing 告警。",
		}
	}
	// 存在 firing 告警时，提示先处理告警再继续其它变更。
	return DailyCheck{
		ID:      "alerts-clear",
		Title:   "检查当前是否存在 firing 告警",
		Status:  "action_required",
		Summary: "当前存在 firing 告警，先处理告警再做其他变更。",
	}
}

// buildRuntimeNodeDailyCheck 生成 runtime node 基础健康巡检项。
// 参数说明：input 是 store 聚合出的可靠性计算输入。
func buildRuntimeNodeDailyCheck(input ReliabilityInputs) DailyCheck {
	// runtime node 没有 offline/not_ready，且没有 bootstrap 超时，认为基础健康巡检通过。
	if input.RuntimeNodesOffline == 0 && input.RuntimeNodesNotReady == 0 && input.RuntimeNodeRegistration.ProvisioningTimedOut == 0 {
		return DailyCheck{
			ID:      "runtime-nodes-ready",
			Title:   "检查 runtime node 是否存在 offline/not_ready 或 bootstrap 超时",
			Status:  "ok",
			Summary: "当前 runtime node 没有 offline/not_ready 节点，也没有长期未完成 bootstrap 的 runtime node。",
		}
	}
	// 默认按 offline/not_ready 节点问题关联 runtime-node-offline runbook。
	runbookID := "runtime-node-offline"
	summary := "存在 offline 或 not_ready runtime node，需要先处理节点健康问题。"
	// bootstrap 超时比普通 not_ready 更具体，优先关联 bootstrap runbook。
	if input.RuntimeNodeRegistration.ProvisioningTimedOut > 0 {
		runbookID = "runtime-node-bootstrap-stuck"
		summary = "存在长期未完成 bootstrap 的 runtime node，需要先处理启动链路、ready 信号或实例状态。"
	}
	// 返回需要处理的 runtime node 巡检项。
	return DailyCheck{
		ID:        "runtime-nodes-ready",
		Title:     "检查 runtime node 是否存在 offline/not_ready 或 bootstrap 超时",
		Status:    "action_required",
		Summary:   summary,
		RunbookID: runbookID,
	}
}

// buildDeliveryDailyCheck 根据发布成功率 SLO 生成发布健康巡检项。
// 参数说明：slo 是发布成功率 SLO 的当前计算结果。
func buildDeliveryDailyCheck(slo SLOStatus) DailyCheck {
	// 发布成功率达标或无样本时，不触发日常巡检行动项。
	if slo.Status == "ok" || slo.Status == "no_data" {
		return DailyCheck{
			ID:      "delivery-slo",
			Title:   "检查最近发布成功率是否健康",
			Status:  "ok",
			Summary: "最近发布成功率没有触发异常。",
		}
	}
	// SLO breached 时，提示检查失败 deployment。
	return DailyCheck{
		ID:        "delivery-slo",
		Title:     "检查最近发布成功率是否健康",
		Status:    "action_required",
		Summary:   "最近 24 小时发布成功率低于目标，需要检查失败 deployment。",
		RunbookID: "deployment-failed",
	}
}
