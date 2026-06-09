package cloudplaneapi

import (
	"database/sql"
	"log/slog"
	"strings"

	cloudplanecontrolapi "mini-cloud/internal/cloudplane/api/controlplane"
	cloudplaneagentapi "mini-cloud/internal/cloudplane/api/nodeagent"
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
	// snapshotService 承载 control-plane 拉取 cloud-plane 快照的内部 API。
	snapshotService := cloudplanecontrolapi.NewSnapshotServer(logger, db, stores, opts.Config, controlPlaneAuth)
	// executionService 只接收 control-plane 已经决定好的执行计划，不保存 service lifecycle truth。
	executionService := cloudplanecontrolapi.NewExecutionServer(logger, stores, controlPlaneAuth)
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
	// 注册 control-plane 内部服务；snapshot 负责 plane-local fact 读取，execution 负责接收已物化执行计划。
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, snapshotService)
	cloudplanev1.RegisterControlPlaneExecutionServiceServer(grpcServer, executionService)
	// 注册 node-agent 内部服务，供运行节点接入 cloud-plane。
	nodeagentv1.RegisterNodeAgentServiceServer(grpcServer, agentService)
	// 返回已注册全部内部服务的 gRPC server，由上层 CLI 负责监听地址和生命周期。
	return grpcServer
}
