package runtimepool

import (
	"errors"
	"strings"
	"time"
)

const (
	StatusProvisioning = "provisioning"
	StatusReady        = "ready"
	StatusTerminating  = "terminating"
	StatusDeleted      = "deleted"
)

var (
	ErrProviderRequired     = errors.New("provider is required")
	ErrRegionRequired       = errors.New("region is required")
	ErrInstanceNameRequired = errors.New("instanceName is required")
	ErrInstanceTypeRequired = errors.New("instanceType is required")
)

// Record 描述 runtimepool 模块中的 record 数据。
type Record struct {
	// ID 是资源唯一标识。
	ID string `json:"id"`
	// Provider 表示云厂商标识。
	Provider string `json:"provider"`
	// Region 表示资源所在地域。
	Region string `json:"region"`
	// InstanceID 表示 instance 的唯一标识。
	InstanceID string `json:"instanceID"`
	// InstanceName 是资源名称或展示名。
	InstanceName string `json:"instanceName"`
	// InstanceType 是云厂商实例规格。
	InstanceType string `json:"instanceType"`
	// NodeID 表示所属 node 的唯一标识。
	NodeID string `json:"nodeID"`
	// Status 是资源当前状态。
	Status string `json:"status"`
	// StatusReason 记录状态变化或失败原因。
	StatusReason string `json:"statusReason"`
	// ProvisionedAt 表示 provider 已接受或完成实例创建的时间。
	ProvisionedAt time.Time `json:"provisionedAt"`
	// ReadyAt 表示对应 node-agent 注册并变为 ready 的时间。
	ReadyAt *time.Time `json:"readyAt,omitempty"`
	// LastSyncedAt 表示最近一次与 provider 或 node-agent 状态对账的时间。
	LastSyncedAt *time.Time `json:"lastSyncedAt,omitempty"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateIntentInput 描述本次操作的输入字段。
type CreateIntentInput struct {
	// Provider 表示云厂商标识。
	Provider string
	// Region 表示资源所在地域。
	Region string
	// InstanceName 是资源名称或展示名。
	InstanceName string
	// InstanceType 是云厂商实例规格。
	InstanceType string
	// StatusReason 记录状态变化或失败原因。
	StatusReason string
	// ProvisionedAt 表示本地记录 provisioning intent 的时间；provider 实际接受或完成创建可能发生在之后。
	ProvisionedAt time.Time
}

type ScaleOutCandidate struct {
	PlanID          string
	ServiceID       string
	CPUMilli        int
	MemoryMi        int
	InstanceName    string
	InstanceType    string
	ClientToken     string
	HasCapacity     bool
	HasProvisioning bool
}

// Validate 校验输入或领域值是否符合 cloud-plane 约束。
func (in CreateIntentInput) Validate() error {
	// intent 创建在 provider API 调用之前，因此不要求 instanceID；provider/region/name/type 是后续创建云实例必须具备的静态输入。
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	if strings.TrimSpace(in.Region) == "" {
		return ErrRegionRequired
	}
	if strings.TrimSpace(in.InstanceName) == "" {
		return ErrInstanceNameRequired
	}
	if strings.TrimSpace(in.InstanceType) == "" {
		return ErrInstanceTypeRequired
	}
	// 领域约束通过后，调用方才适合进入持久化流程。
	return nil
}
