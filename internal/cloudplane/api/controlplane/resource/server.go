package resource

import (
	"log/slog"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

// Server 实现 resource southbound gRPC 服务。
type Server struct {
	// UnimplementedControlPlaneResourceServiceServer 嵌入 proto 生成的前向兼容实现。
	cloudplanev1.UnimplementedControlPlaneResourceServiceServer

	// logger 记录 resource gRPC handler 运行日志。
	logger *slog.Logger
	// store 提供全局 resource 状态访问。
	store *store.Store
	// auth 校验 control-plane southbound bearer token。
	auth controlplane.Authenticator
}

// NewServer 构造 resource southbound gRPC service。
// 参数说明：logger 记录该组件的结构化日志；stores 提供本地状态访问；auth 校验 control-plane 内部 token。
func NewServer(logger *slog.Logger, stores *store.Store, auth controlplane.Authenticator) cloudplanev1.ControlPlaneResourceServiceServer {
	return &Server{logger: logger, store: stores, auth: auth}
}
