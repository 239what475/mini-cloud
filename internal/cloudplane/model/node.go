// Package node 定义 cloud-plane 视角下的节点注册、心跳和健康状态模型。
package model

import (
	"errors"
	"net"
	"strings"
	"time"
)

const (
	// StatusRegistering 表示 node-agent 已开始注册但尚未 ready。
	StatusRegistering = "registering"
	// StatusReady 表示 node 从健康状态上已 ready；是否可调度还需结合 schedulable 和资源余量判断。
	StatusReady = "ready"
	// StatusNotReady 表示 node 已注册但暂时不可执行 workload。
	StatusNotReady = "not_ready"
	// StatusDraining 表示 node 正在排空，不能接收新调度。
	StatusDraining = "draining"
	// StatusOffline 表示 node 心跳超时或被判定离线。
	StatusOffline = "offline"
)

var (
	// ErrProviderRequired 表示节点输入缺少云厂商标识。
	ErrProviderRequired = errors.New("provider is required")
	// ErrRegionRequired 表示节点输入缺少地域。
	ErrRegionRequired = errors.New("region is required")
	// ErrNodeNameRequired 表示节点输入缺少名称。
	ErrNodeNameRequired = errors.New("name is required")
	// ErrPrivateIPRequired 表示节点输入缺少内网 IP。
	ErrPrivateIPRequired = errors.New("privateIP is required")
	// ErrInvalidPrivateIP 表示内网 IP 不是合法 IP 地址。
	ErrInvalidPrivateIP = errors.New("privateIP must be a valid IP address")
	// ErrInvalidPublicIP 表示公网 IP 不是合法 IP 地址。
	ErrInvalidPublicIP = errors.New("publicIP must be a valid IP address")
	// ErrInstanceIDRequired 表示节点输入缺少云实例标识。
	ErrInstanceIDRequired = errors.New("instanceID is required")
	// ErrInstanceTypeRequired 表示节点输入缺少云实例规格。
	ErrInstanceTypeRequired = errors.New("instanceType is required")
	// ErrInvalidCPUMilliTotal 表示节点上报的 CPU 总毫核数非法。
	ErrInvalidCPUMilliTotal = errors.New("cpuMilliTotal must be greater than 0")
	// ErrInvalidMemoryMiTotal 表示节点上报的内存总 MiB 数非法。
	ErrInvalidMemoryMiTotal = errors.New("memoryMiTotal must be greater than 0")
	// ErrReportedAtRequired 表示心跳输入缺少上报时间。
	ErrReportedAtRequired = errors.New("reportedAt is required")
	// ErrAgentVersionRequired 表示心跳输入缺少 node-agent 版本。
	ErrAgentVersionRequired = errors.New("agentVersion is required")
	// ErrInvalidCPUMilliAllocatable 表示节点可调度 CPU 毫核数非法。
	ErrInvalidCPUMilliAllocatable = errors.New("cpuMilliAllocatable must be greater than 0")
	// ErrInvalidMemoryMiAllocatable 表示节点可调度内存 MiB 数非法。
	ErrInvalidMemoryMiAllocatable = errors.New("memoryMiAllocatable must be greater than 0")
	// ErrInvalidRunningContainers 表示运行中容器数量不能为负数。
	ErrInvalidRunningContainers = errors.New("runningContainers must be greater than or equal to 0")
	// ErrInvalidStatus 表示节点状态不属于允许集合。
	ErrInvalidStatus = errors.New("status must be one of registering, ready, not_ready, draining, offline")
)

// Node 描述 cloud-plane 管理的 runtime node 状态。
type Node struct {
	// ID 是 node 记录的唯一标识。
	ID string `json:"id"`
	// Provider 表示云厂商标识。
	Provider string `json:"provider"`
	// Region 是 node 所在地域。
	Region string `json:"region"`
	// Name 是 node 的机器可读名称。
	Name string `json:"name"`
	// PrivateIP 是节点内网地址。
	PrivateIP string `json:"privateIP"`
	// PublicIP 是节点公网地址。
	PublicIP string `json:"publicIP"`
	// InstanceID 表示 instance 的唯一标识。
	InstanceID string `json:"instanceID"`
	// InstanceType 是云厂商实例规格。
	InstanceType string `json:"instanceType"`
	// CPUMilliTotal 是节点上报给 cloud-plane 的 CPU 总毫核数。
	CPUMilliTotal int `json:"cpuMilliTotal"`
	// MemoryMiTotal 是节点上报给 cloud-plane 的内存总 MiB 数。
	MemoryMiTotal int `json:"memoryMiTotal"`
	// CPUMilliAllocatable 表示节点可供 workload 调度的 CPU 毫核数。
	CPUMilliAllocatable int `json:"cpuMilliAllocatable"`
	// MemoryMiAllocatable 表示节点可供 workload 调度的内存 MiB 数。
	MemoryMiAllocatable int `json:"memoryMiAllocatable"`
	// CPUMilliAllocated 表示已被 cloud-plane selection 占用的 CPU 毫核数。
	CPUMilliAllocated int `json:"cpuMilliAllocated"`
	// MemoryMiAllocated 表示已被 cloud-plane selection 占用的内存 MiB 数。
	MemoryMiAllocated int `json:"memoryMiAllocated"`
	// Status 是 node 当前状态。
	Status string `json:"status"`
	// Schedulable 表示节点是否允许接受新的 workload 调度。
	Schedulable bool `json:"schedulable"`
	// LastHeartbeatAt 是 cloud-plane 最近一次接收到该 node 心跳的时间。
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// HeartbeatSummary 是 node-agent 最近一次心跳上报摘要。
type HeartbeatSummary struct {
	// ReportedAt 是 node-agent 生成本次心跳数据的时间。
	ReportedAt time.Time `json:"reportedAt"`
	// AgentVersion 是 node-agent 上报的版本。
	AgentVersion string `json:"agentVersion"`
	// CPUMilliAllocatable 表示节点可供 workload 调度的 CPU 毫核数。
	CPUMilliAllocatable int `json:"cpuMilliAllocatable"`
	// MemoryMiAllocatable 表示节点可供 workload 调度的内存 MiB 数。
	MemoryMiAllocatable int `json:"memoryMiAllocatable"`
	// RunningContainers 是 node-agent 观测到的正在运行容器数量。
	RunningContainers int `json:"runningContainers"`
	// Status 是最近一次心跳上报的 node 状态。
	Status string `json:"status"`
}

// ReconcileImpact 描述 stale heartbeat 对 execution plan 和 service 的影响。
type ReconcileImpact struct {
	// NodeID 是触发本次影响的 stale 或 offline node 标识。
	NodeID string `json:"nodeID"`
	// NodeName 表示 node 名称。
	NodeName string `json:"nodeName"`
	// PlanID 表示受影响的 execution plan 唯一标识。
	PlanID string `json:"planID"`
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// ServiceName 表示 service 名称。
	ServiceName string `json:"serviceName"`
	// Reason 说明该 stale node 导致 execution plan 受影响的原因。
	Reason string `json:"reason"`
}

// HeartbeatReconcileResult 汇总一次 stale heartbeat 巡检造成的状态变化。
type HeartbeatReconcileResult struct {
	// StaleAfterSeconds 是本次心跳巡检使用的 stale 判定窗口秒数。
	StaleAfterSeconds int `json:"staleAfterSeconds"`
	// CutoffTime 是本次巡检判定心跳过期使用的截止时间。
	CutoffTime time.Time `json:"cutoffTime"`
	// NodesMarkedOffline 表示本次心跳巡检中被标记为 offline 的 node 列表。
	NodesMarkedOffline []Node `json:"nodesMarkedOffline"`
	// ImpactedPlans 是因 stale node 被标记失败的 execution plan 列表。
	ImpactedPlans []ReconcileImpact `json:"impactedPlans"`
}

// RegisterInput 是 node-agent 首次注册节点时提供的身份和容量信息。
type RegisterInput struct {
	// Provider 表示云厂商标识。
	Provider string `json:"provider"`
	// Region 是 node 所在地域。
	Region string `json:"region"`
	// Name 是 node 的机器可读名称。
	Name string `json:"name"`
	// PrivateIP 是节点内网地址。
	PrivateIP string `json:"privateIP"`
	// PublicIP 是节点公网地址。
	PublicIP string `json:"publicIP"`
	// InstanceID 表示 instance 的唯一标识。
	InstanceID string `json:"instanceID"`
	// InstanceType 是云厂商实例规格。
	InstanceType string `json:"instanceType"`
	// CPUMilliTotal 是节点上报给 cloud-plane 的 CPU 总毫核数。
	CPUMilliTotal int `json:"cpuMilliTotal"`
	// MemoryMiTotal 是节点上报给 cloud-plane 的内存总 MiB 数。
	MemoryMiTotal int `json:"memoryMiTotal"`
}

// Validate 校验节点注册输入是否满足 cloud-plane 领域约束。
func (in RegisterInput) Validate() error {
	// provider 决定该 node 属于哪个云厂商驱动管理范围。
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	// region 用于 scheduler 按 service region 过滤 runtime node。
	if strings.TrimSpace(in.Region) == "" {
		return ErrRegionRequired
	}
	// name 是 node 的机器可读标识，注册时必须上报。
	if strings.TrimSpace(in.Name) == "" {
		return ErrNodeNameRequired
	}
	// private IP 是 cloud-plane 或 node-agent 间内网通信和探测的基础地址。
	if strings.TrimSpace(in.PrivateIP) == "" {
		return ErrPrivateIPRequired
	}
	// private IP 只做 IP 字面量合法性校验，不验证网络可达性。
	if net.ParseIP(strings.TrimSpace(in.PrivateIP)) == nil {
		return ErrInvalidPrivateIP
	}
	// public IP 可选；配置时同样只校验 IP 字面量合法性。
	if publicIP := strings.TrimSpace(in.PublicIP); publicIP != "" && net.ParseIP(publicIP) == nil {
		return ErrInvalidPublicIP
	}
	// instanceID 用于把 node-agent 注册记录和云厂商实例关联。
	if strings.TrimSpace(in.InstanceID) == "" {
		return ErrInstanceIDRequired
	}
	// instanceType 用于平台视图和 runtime node 规格排查。
	if strings.TrimSpace(in.InstanceType) == "" {
		return ErrInstanceTypeRequired
	}
	// 总 CPU 必须为正数；该值来自 node-agent 容量计算或配置。
	if in.CPUMilliTotal <= 0 {
		return ErrInvalidCPUMilliTotal
	}
	// 总内存必须为正数；单位是 MiB。
	if in.MemoryMiTotal <= 0 {
		return ErrInvalidMemoryMiTotal
	}
	// 注册输入满足领域约束后，调用方才适合进入 store 写入流程。
	return nil
}

// HeartbeatInput 是 node-agent 周期性上报的节点容量和状态信息。
type HeartbeatInput struct {
	// ReportedAt 是 node-agent 生成本次心跳数据的时间。
	ReportedAt time.Time `json:"reportedAt"`
	// AgentVersion 是 node-agent 上报的版本。
	AgentVersion string `json:"agentVersion"`
	// CPUMilliAllocatable 表示节点可供 workload 调度的 CPU 毫核数。
	CPUMilliAllocatable int `json:"cpuMilliAllocatable"`
	// MemoryMiAllocatable 表示节点可供 workload 调度的内存 MiB 数。
	MemoryMiAllocatable int `json:"memoryMiAllocatable"`
	// RunningContainers 是 node-agent 观测到的正在运行容器数量。
	RunningContainers int `json:"runningContainers"`
	// Status 是 node-agent 上报的 node 状态。
	Status string `json:"status"`
}

// Validate 校验节点心跳输入是否满足 cloud-plane 领域约束。
func (in HeartbeatInput) Validate() error {
	// reportedAt 是 node-agent 观测时间，不能为空。
	if in.ReportedAt.IsZero() {
		return ErrReportedAtRequired
	}
	// agentVersion 用于排障和版本分布统计。
	if strings.TrimSpace(in.AgentVersion) == "" {
		return ErrAgentVersionRequired
	}
	// 可调度 CPU 必须为正数，不能为 0 或负数。
	if in.CPUMilliAllocatable <= 0 {
		return ErrInvalidCPUMilliAllocatable
	}
	// 可调度内存必须为正数，单位 MiB。
	if in.MemoryMiAllocatable <= 0 {
		return ErrInvalidMemoryMiAllocatable
	}
	// 运行中容器数量是观测值，可以为 0，但不能为负数。
	if in.RunningContainers < 0 {
		return ErrInvalidRunningContainers
	}
	// 心跳状态必须属于 node 状态机。
	if !IsNodeStatus(in.Status) {
		return ErrInvalidStatus
	}
	// 通过领域校验后，调用方才适合进入心跳持久化流程。
	return nil
}

// IsStatus 判断节点状态是否属于当前允许值。
func IsNodeStatus(status string) bool {
	switch status {
	case StatusRegistering, StatusReady, StatusNotReady, StatusDraining, StatusOffline:
		return true
	default:
		return false
	}
}
