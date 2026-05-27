package cloudplaneapi

import (
	"database/sql"
	"log/slog"
	"strings"

	cloudplanecontrolapi "mini-cloud/internal/cloudplane/api/controlplane"
	cloudplaneprojectapi "mini-cloud/internal/cloudplane/api/controlplane/project"
	cloudplaneserviceapi "mini-cloud/internal/cloudplane/api/controlplane/service"
	cloudplanesnapshotapi "mini-cloud/internal/cloudplane/api/controlplane/snapshot"
	cloudplaneagentapi "mini-cloud/internal/cloudplane/api/nodeagent"
	"mini-cloud/internal/cloudplane/control/lifecycle"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc"
)

// NewGRPCServer 创建 cloud-plane 内部 gRPC 服务端。
// 参数说明：opts 是服务构造所需配置和依赖；logger 记录服务端运行日志；db 提供数据库访问；stores 聚合各领域 store。
func NewGRPCServer(opts Options, logger *slog.Logger, db *sql.DB, stores *store.Store) *grpc.Server {
	// controlPlaneAuth 校验 control-plane 到 cloud-plane 的内部 southbound 调用身份。
	controlPlaneAuth := cloudplanecontrolapi.NewAuthenticator(opts.Config.ControlPlane.Auth.BearerToken)
	// lifecycle controller 封装 workload service 查询、删除和 rollout 等同步流程。
	lifecycleControllers := lifecycle.NewControllers(logger, stores, lifecycle.Options{
		Config:        opts.Config,
		RuntimeDriver: opts.RuntimeDriver,
	})
	// snapshotService 承载 control-plane 拉取 cloud-plane 快照的内部 API。
	snapshotService := cloudplanesnapshotapi.NewServer(logger, db, stores, opts.Config, controlPlaneAuth)
	// projectService 承载 control-plane 同步 project 和 project resource 的内部 API。
	projectService := cloudplaneprojectapi.NewServer(logger, stores, controlPlaneAuth)
	// workloadService 承载 control-plane 同步、查询和回滚 workload service 的内部 API。
	workloadService := cloudplaneserviceapi.NewServer(logger, stores, controlPlaneAuth, lifecycleControllers)
	// agentService 承载 node-agent 注册、心跳、拉取 work item 和上报 execution 的内部 API。
	agentService := cloudplaneagentapi.NewServer(
		logger,
		stores,
		// BootstrapToken 是 node-agent 首次注册节点时使用的入口凭据；注册前先裁剪空白。
		strings.TrimSpace(opts.Config.NodeAgent.Auth.BootstrapToken),
		// SessionTTL 控制 node-agent 注册后获得的会话 token 有效期。
		opts.Config.NodeAgent.Auth.SessionTTL,
	)

	// 创建裸 gRPC server；当前 cloud-plane 不在这里挂载 HTTP gateway 或额外拦截器。
	grpcServer := grpc.NewServer()
	// 注册 control-plane 内部服务；project、workload、snapshot 拆成独立 gRPC service，避免单个接口混杂多类职责。
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, snapshotService)
	cloudplanev1.RegisterControlPlaneProjectServiceServer(grpcServer, projectService)
	cloudplanev1.RegisterControlPlaneWorkloadServiceServer(grpcServer, workloadService)
	// 注册 node-agent 内部服务，供运行节点接入 cloud-plane。
	nodeagentv1.RegisterNodeAgentServiceServer(grpcServer, agentService)
	// 返回已注册全部内部服务的 gRPC server，由上层 CLI 负责监听地址和生命周期。
	return grpcServer
}
