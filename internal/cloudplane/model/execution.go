// Package execution 定义 node-agent 执行 execution work item 的领域模型。
package model

import (
	"errors"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
)

const (
	// StatusPending 表示 execution intent 已创建但还未被 node-agent 领取。
	StatusPending = "pending"
	// StatusDeploying 表示 node-agent 正在创建或更新该容器。
	StatusDeploying = "deploying"
	// StatusRunning 表示该容器已启动并通过运行时上报。
	StatusRunning = "running"
	// StatusSuperseded 表示该 execution 已被新 execution 替代。
	StatusSuperseded = "superseded"
	// StatusFailed 表示该 execution 执行失败并进入终态。
	StatusFailed = "failed"
)

var (
	// ErrExecutionStatusInvalid 表示 execution 上报状态不属于允许值。
	ErrExecutionStatusInvalid = errors.New("status must be one of deploying, running, superseded, failed")
	// ErrExecutionIDRequired 表示操作缺少 execution 标识。
	ErrExecutionIDRequired = errors.New("executionID is required")
	// ErrReasonRequired 表示 execution 上报缺少状态原因。
	ErrReasonRequired = errors.New("reason is required")
	// ErrContainerNameRequired 表示 execution 上报缺少容器名称。
	ErrContainerNameRequired = errors.New("containerName is required")
	// ErrHostPortInvalid 表示 running 状态缺少有效宿主机端口。
	ErrHostPortInvalid           = errors.New("hostPort must be greater than 0 when status is running")
	ErrPlanIDRequired            = errors.New("planID is required")
	ErrServiceIDRequired         = errors.New("serviceID is required")
	ErrServiceGenerationRequired = errors.New("serviceGeneration must be greater than 0")
	ErrServiceNameRequired       = errors.New("serviceName is required")
	ErrImageRequired             = errors.New("image is required")
	ErrInvalidContainerPort      = errors.New("containerPort must be between 1 and 65535")
)

const (
	PlanActionCreated = "created"
	PlanActionUpdated = "updated"

	WorkActionRun    = "run"
	WorkActionDelete = "delete"
)

type PlanInput struct {
	PlanID            string               `json:"planID"`
	ServiceID         string               `json:"serviceID"`
	ServiceName       string               `json:"serviceName"`
	ServiceGeneration int64                `json:"serviceGeneration"`
	Image             string               `json:"image"`
	Command           []string             `json:"command"`
	Args              []string             `json:"args"`
	Env               map[string]string    `json:"env"`
	ProjectedFiles    []projectedfile.File `json:"projectedFiles,omitempty"`
	ImageCredential   *ImageCredential     `json:"imageCredential,omitempty"`
	ContainerPort     int                  `json:"containerPort"`
	ReadinessPath     string               `json:"readinessPath"`
	InstanceClass     string               `json:"instanceClass"`
	Exposure          string               `json:"exposure"`
}

type PlanResult struct {
	Action string `json:"action"`
	PlanID string `json:"planID"`
}

type DeletePlanInput struct {
	ServiceID         string `json:"serviceID"`
	ServiceGeneration int64  `json:"serviceGeneration"`
	PlanID            string `json:"planID"`
}

func (in DeletePlanInput) Validate() error {
	if strings.TrimSpace(in.ServiceID) == "" {
		return ErrServiceIDRequired
	}
	if strings.TrimSpace(in.PlanID) == "" {
		return ErrPlanIDRequired
	}
	if in.ServiceGeneration <= 0 {
		return ErrServiceGenerationRequired
	}
	return nil
}

func (in PlanInput) Validate() error {
	if strings.TrimSpace(in.PlanID) == "" {
		return ErrPlanIDRequired
	}
	if strings.TrimSpace(in.ServiceID) == "" {
		return ErrServiceIDRequired
	}
	if strings.TrimSpace(in.ServiceName) == "" {
		return ErrServiceNameRequired
	}
	if strings.TrimSpace(in.Image) == "" {
		return ErrImageRequired
	}
	if in.ContainerPort <= 0 || in.ContainerPort > 65535 {
		return ErrInvalidContainerPort
	}
	for _, item := range projectedfile.CloneFiles(in.ProjectedFiles) {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// WorkItem 是 cloud-plane 下发给 node-agent 执行的服务实例任务。
type WorkItem struct {
	// Action 表示该 work item 的处理类型。
	Action string `json:"action"`
	// ExecutionID 表示 execution 的唯一标识。
	ExecutionID string `json:"executionID"`
	// PlanID 表示所属 execution plan 的唯一标识。
	PlanID string `json:"planID"`
	// NodeID 是该任务被分配到的 node 标识。
	NodeID string `json:"nodeID"`
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// ServiceName 表示 service 名称。
	ServiceName string `json:"serviceName"`
	// Image 表示容器镜像。
	Image string `json:"image"`
	// Command 表示容器启动命令。
	Command []string `json:"command"`
	// Args 表示容器启动参数。
	Args []string `json:"args"`
	// Env 表示容器环境变量。
	Env map[string]string `json:"env"`
	// ProjectedFiles 表示由 config/secret 渲染到容器内的文件列表。
	ProjectedFiles []projectedfile.File `json:"projectedFiles,omitempty"`
	// ImageCredential 记录容器镜像或镜像凭据相关信息。
	ImageCredential *ImageCredential `json:"imageCredential,omitempty"`
	// SupersededExecution 是被当前执行替换掉的旧执行信息。
	SupersededExecution *SupersededExecution `json:"supersededExecution,omitempty"`
	// ContainerPort 表示容器内部监听端口。
	ContainerPort int `json:"containerPort"`
	// ReadinessPath 表示就绪探测路径。
	ReadinessPath string `json:"readinessPath"`
	// ContainerName 表示容器运行时中的容器名称。
	ContainerName string `json:"containerName"`
	// ContainerID 表示需要清理或报告的容器运行时标识。
	ContainerID string `json:"containerID"`
	// HostPort 表示已有 execution 占用的宿主机端口。
	HostPort int `json:"hostPort"`
}

// ImageCredential 是 node-agent 拉取容器镜像时使用的仓库凭据。
type ImageCredential struct {
	// Server 是镜像仓库服务地址。
	Server string `json:"server"`
	// Username 是访问镜像仓库的用户名。
	Username string `json:"username"`
	// Password 表示拉取镜像时使用的敏感密码，只下发给执行节点，不应在控制面响应中回显。
	Password string `json:"password"`
}

// SupersededExecution 描述被新 execution 取代并需要清理的旧执行。
type SupersededExecution struct {
	// PlanID 表示所属 execution plan 的唯一标识。
	PlanID string `json:"planID"`
	// ExecutionID 表示 execution 的唯一标识。
	ExecutionID string `json:"executionID"`
	// ContainerID 表示容器运行时返回的容器标识。
	ContainerID string `json:"containerID"`
	// ContainerName 表示容器运行时中的容器名称。
	ContainerName string `json:"containerName"`
}

// Record 是 cloud-plane 持久化的单个 execution 运行记录。
type Record struct {
	// ID 是 execution 记录的唯一标识。
	ID string `json:"id"`
	// PlanID 表示所属 execution plan 的唯一标识。
	PlanID string `json:"planID"`
	// NodeID 是该 execution 实际运行所在的 node 标识。
	NodeID string `json:"nodeID"`
	// Image 表示容器镜像。
	Image string `json:"image"`
	// ContainerName 表示容器运行时中的容器名称。
	ContainerName string `json:"containerName"`
	// ContainerID 表示容器运行时返回的容器标识。
	ContainerID string `json:"containerID"`
	// ContainerPort 表示容器内部监听端口。
	ContainerPort int `json:"containerPort"`
	// HostPort 表示 node 宿主机上为该执行分配的端口。
	HostPort int `json:"hostPort"`
	// ReadinessPath 表示就绪探测路径。
	ReadinessPath string `json:"readinessPath"`
	// Status 是 execution 当前状态。
	Status string `json:"status"`
	// StatusReason 记录状态变化或失败原因。
	StatusReason string `json:"statusReason"`
	// StartedAt 表示任务开始时间。
	StartedAt time.Time `json:"startedAt"`
	// FinishedAt 是 execution 进入终态的时间；仍在运行时为空。
	FinishedAt *time.Time `json:"finishedAt"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// ReportInput 是 node-agent 上报 execution 结果时提供的字段。
type ReportInput struct {
	// Status 是 node-agent 上报的 execution 状态。
	Status string `json:"status"`
	// Reason 是 node-agent 对本次 execution 结果的说明或失败原因。
	Reason string `json:"reason"`
	// ContainerID 表示容器运行时返回的容器标识。
	ContainerID string `json:"containerID"`
	// ContainerName 表示容器运行时中的容器名称。
	ContainerName string `json:"containerName"`
	// HostPort 表示 node 宿主机上为该执行分配的端口。
	HostPort int `json:"hostPort"`
	// SupersededExecutionID 表示 superseded execution 的唯一标识。
	SupersededExecutionID string `json:"supersededExecutionID"`
}

// ReportAck 是 cloud-plane 接受 execution 上报后的确认视图。
type ReportAck struct {
	// Execution 是本次上报后持久化的 execution 记录。
	Execution Record `json:"execution"`
	// ObservedAt 是 cloud-plane 持久化本次上报的时间。
	ObservedAt time.Time `json:"observedAt"`
}

// IsStatus 判断 execution 状态是否属于当前允许值。
func IsExecutionStatus(status string) bool {
	switch status {
	case StatusDeploying, StatusRunning, StatusSuperseded, StatusFailed:
		return true
	default:
		return false
	}
}

func NormalizeWorkAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", WorkActionRun:
		return WorkActionRun
	case WorkActionDelete:
		return WorkActionDelete
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
}

func IsWorkAction(action string) bool {
	switch NormalizeWorkAction(action) {
	case WorkActionRun, WorkActionDelete:
		return true
	default:
		return false
	}
}

// Validate 校验 node-agent execution 上报是否满足领域约束。
func (in ReportInput) Validate() error {
	// node-agent 上报的状态必须在 execution 状态机允许集合内。
	if !IsExecutionStatus(in.Status) {
		return ErrExecutionStatusInvalid
	}
	// reason 是每次上报的可读说明，running/failed 等状态都要求携带。
	if strings.TrimSpace(in.Reason) == "" {
		return ErrReasonRequired
	}
	// containerName 是 cloud-plane 后续定位和清理容器的关键字段。
	if strings.TrimSpace(in.ContainerName) == "" {
		return ErrContainerNameRequired
	}
	// 只有 running 状态需要上报正数 host port；其它状态不要求端口为正。
	if in.Status == StatusRunning && in.HostPort <= 0 {
		return ErrHostPortInvalid
	}
	// 所有领域约束通过后，调用方才适合进入持久化流程。
	return nil
}
