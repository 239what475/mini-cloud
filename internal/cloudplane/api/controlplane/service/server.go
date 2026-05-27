package service

import (
	"log/slog"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/control/lifecycle"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

// Server 实现 cloud-plane 暴露给 control-plane 的 workload southbound gRPC 服务。
type Server struct {
	// UnimplementedControlPlaneWorkloadServiceServer 嵌入 proto 生成的前向兼容实现。
	cloudplanev1.UnimplementedControlPlaneWorkloadServiceServer

	// logger 记录 workload gRPC handler 运行日志。
	logger *slog.Logger
	// store 提供 workload southbound API 需要的本地状态访问。
	store *store.Store
	// auth 校验 control-plane southbound bearer token。
	auth controlplane.Authenticator
	// lifecycle 执行 service lifecycle 业务操作。
	lifecycle lifecycle.Controllers
}

// NewServer 构造 workload southbound gRPC service。
// 参数说明：logger 记录该组件的结构化日志；stores 提供 cloud-plane 聚合状态访问；auth 校验 control-plane 内部 token；controllers 执行 service lifecycle 业务操作。
func NewServer(logger *slog.Logger, stores *store.Store, auth controlplane.Authenticator, controllers lifecycle.Controllers) cloudplanev1.ControlPlaneWorkloadServiceServer {
	return &Server{logger: logger, store: stores, auth: auth, lifecycle: controllers}
}
