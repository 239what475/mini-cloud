// Package deployment 定义 service revision 发布后的 deployment 状态机和值对象。
package deployment

import (
	"errors"
	"fmt"
	"time"
)

const (
	// StatusPending 表示 deployment 已创建但尚未进入调度。
	StatusPending = "pending"
	// StatusScheduling 表示 deployment 正在为副本选择 runtime node。
	StatusScheduling = "scheduling"
	// StatusAssigned 表示 deployment 已生成 selection decision，等待下发执行。
	StatusAssigned = "assigned"
	// StatusDeploying 表示 deployment 已下发到 node-agent 并正在启动副本。
	StatusDeploying = "deploying"
	// StatusRunning 表示 deployment 的副本已达到期望运行状态。
	StatusRunning = "running"
	// StatusSuperseded 表示 deployment 已被更新的 revision 替代。
	StatusSuperseded = "superseded"
	// StatusFailed 表示 deployment 发布失败并进入终态。
	StatusFailed = "failed"
)

var (
	// ErrDesiredReplicasInvalid 表示 deployment 期望副本数不是正数。
	ErrDesiredReplicasInvalid = errors.New("desiredReplicas must be greater than 0")
	// ErrDeploymentStatusInvalid 表示状态值不属于 deployment 状态机。
	ErrDeploymentStatusInvalid = errors.New("invalid deployment status")
	// ErrTransitionReasonRequired 表示状态流转缺少原因说明。
	ErrTransitionReasonRequired = errors.New("transition reason is required")
	// ErrInvalidStatusTransition 表示状态机不允许本次流转。
	ErrInvalidStatusTransition = errors.New("invalid deployment status transition")
)

// Deployment 表示一次 revision 发布对应的副本部署记录。
type Deployment struct {
	// ID 是 deployment 记录的唯一标识。
	ID string `json:"id"`
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// RevisionID 表示所属 revision 的唯一标识。
	RevisionID string `json:"revisionID"`
	// DesiredReplicas 表示 deployment 期望副本数。
	DesiredReplicas int `json:"desiredReplicas"`
	// ReadyReplicas 表示已通过运行时上报的 ready 副本数。
	ReadyReplicas int `json:"readyReplicas"`
	// AvailableReplicas 表示可对外承载流量的副本数。
	AvailableReplicas int `json:"availableReplicas"`
	// Status 是 deployment 当前状态。
	Status string `json:"status"`
	// StatusReason 表示最近一次 deployment 状态变化的原因。
	StatusReason string `json:"statusReason"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateInput 是创建 deployment 时必须提供的领域输入。
type CreateInput struct {
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string
	// RevisionID 表示所属 revision 的唯一标识。
	RevisionID string
	// DesiredReplicas 表示 deployment 期望副本数。
	DesiredReplicas int
}

// Validate 校验创建 deployment 所需字段是否满足领域约束。
func (in CreateInput) Validate() error {
	if in.DesiredReplicas <= 0 {
		return ErrDesiredReplicasInvalid
	}
	return nil
}

// IsStatus 判断 deployment 状态是否属于当前状态机允许值。
func IsStatus(status string) bool {
	switch status {
	case StatusPending, StatusScheduling, StatusAssigned, StatusDeploying, StatusRunning, StatusSuperseded, StatusFailed:
		return true
	default:
		return false
	}
}

// ValidateTransition 校验 deployment 状态流转是否合法。
// 参数说明：from 是原状态；to 是目标状态；reason 记录触发本次流转的原因。
func ValidateTransition(from string, to string, reason string) error {
	// from/to 都必须属于当前状态机定义的状态集合。
	if !IsStatus(from) || !IsStatus(to) {
		return ErrDeploymentStatusInvalid
	}
	// 每次状态流转都必须携带原因，便于审计和排障。
	if reason == "" {
		return ErrTransitionReasonRequired
	}

	// 根据当前状态枚举允许的下一跳状态。
	switch from {
	case StatusPending:
		// pending 只能进入调度，或在调度前直接失败。
		if to == StatusScheduling || to == StatusFailed {
			return nil
		}
	case StatusScheduling:
		// scheduling 成功后进入 assigned，失败则进入 failed。
		if to == StatusAssigned || to == StatusFailed {
			return nil
		}
	case StatusAssigned:
		// assigned 表示 selection 已生成，下一步是 node-agent 开始部署。
		if to == StatusDeploying || to == StatusFailed {
			return nil
		}
	case StatusDeploying:
		// deploying 成功后进入 running，启动失败则进入 failed。
		if to == StatusRunning || to == StatusFailed {
			return nil
		}
	case StatusRunning:
		// running deployment 可以被新 revision 替代，也可能因节点或执行异常进入 failed。
		if to == StatusSuperseded || to == StatusFailed {
			return nil
		}
	}

	// 未命中任何允许边时，返回带 from/to 的状态机错误。
	return fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, from, to)
}
