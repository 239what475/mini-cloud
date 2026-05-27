package lifecycle

import (
	"errors"
	"log/slog"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/revision"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
)

var (
	// ErrServiceIDRequired 表示调用方没有提供 service ID。
	ErrServiceIDRequired = errors.New("serviceID is required")
	// ErrScaleDownNotSupported 表示当前控制器尚不支持 service 副本数缩小。
	ErrScaleDownNotSupported = errors.New("service replica scale-down is not supported")
	// ErrScaleDuringRollout 表示存在候选 revision 时不允许同时调整副本数。
	ErrScaleDuringRollout = errors.New("service replicas cannot be changed while a candidate revision rollout exists")
)

// Options 描述 lifecycle 控制器构造所需依赖。
type Options struct {
	// Config 是 cloud-plane 的有效配置。
	Config cloudplaneconfig.Config
	// RuntimeDriver 是创建 runtime node 所需的云厂商驱动。
	RuntimeDriver runtimepool.RuntimeDriver
}

// Controllers 汇总 service lifecycle 的变更控制入口。
type Controllers struct {
	// Mutations 提供 service create/update/delete 等变更入口。
	Mutations ServiceMutationController
}

type controllerCore struct {
	logger            *slog.Logger
	store             *store.Store
	config            cloudplaneconfig.Config
	deploymentService deploymentCoordinator
}

// ServiceQueryController 提供 lifecycle 内部运行态计算能力。
type ServiceQueryController struct{ *controllerCore }

// ServiceMutationController 提供 service create/update/delete 等变更入口。
type ServiceMutationController struct{ *controllerCore }

// RuntimeState 描述 service 当前运行态和 rollout 观测状态。
type RuntimeState struct {
	// CurrentRevisionID 是当前正式承载流量的 revision ID。
	CurrentRevisionID string
	// CurrentDeploymentID 是当前正式 deployment ID。
	CurrentDeploymentID string
	// CurrentRevision 是当前正式 revision 详情。
	CurrentRevision *revision.Revision
	// CandidateRevision 是正在发布或等待处理的候选 revision。
	CandidateRevision *revision.Revision
	// CurrentDeployment 是当前正式 deployment 详情。
	CurrentDeployment *deployment.Deployment
	// CurrentExecution 是当前代表性 execution 详情。
	CurrentExecution *execution.Record
	// Healthy 表示当前 service 是否达到健康运行状态。
	Healthy bool
	// Message 是当前运行态或 rollout 的用户可读说明。
	Message string
}

// LaunchResult 描述一次 revision launch 创建出的 deployment、service 和 selection 结果。
type LaunchResult struct {
	// Revision 是本次 launch 使用的 revision。
	Revision revision.Revision
	// Deployment 是本次 launch 创建或推进的 deployment。
	Deployment deployment.Deployment
	// Service 是 launch 后更新出的 service 状态。
	Service workload.Service
	// Placement 是代表性的持久化 placement；多副本场景只返回其中一个用于兼容调用视图。
	Placement *scheduler.StoredDecision
}

// CreateResult 描述 service create 后的运行态和首个 launch 结果。
type CreateResult struct {
	// Runtime 是创建前或创建过程中的 service 运行态。
	Runtime RuntimeState
	// Launch 是首个 revision launch 结果。
	Launch LaunchResult
}

// UpdateResult 描述 service update 后的运行态、service 状态和可能触发的 launch。
type UpdateResult struct {
	// Runtime 是更新前或更新过程中的 service 运行态。
	Runtime RuntimeState
	// Service 是更新后的 service 持久化记录。
	Service workload.Service
	// RolloutTriggered 表示本次更新是否触发了新的候选 revision 发布。
	RolloutTriggered bool
	// Launch 是本次更新触发的 launch 结果；未触发 rollout 时为空。
	Launch *LaunchResult
}

// NewControllers 构造 service lifecycle 控制器集合。
// 参数说明：logger 记录内部控制流程日志；stores 提供本地状态访问；opts 提供配置和 runtime driver。
func NewControllers(logger *slog.Logger, stores *store.Store, opts Options) Controllers {
	core := &controllerCore{
		logger: logger,
		store:  stores,
		config: opts.Config,
	}
	core.deploymentService = newDeploymentCoordinator(logger, stores, opts.Config, opts.RuntimeDriver)
	return Controllers{
		Mutations: ServiceMutationController{core},
	}
}
