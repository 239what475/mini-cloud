package snapshot

import (
	"database/sql"
	"log/slog"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

// Server 实现 snapshot southbound gRPC 服务。
type Server struct {
	// UnimplementedControlPlaneSnapshotServiceServer 嵌入 proto 生成的前向兼容实现。
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer

	// logger 记录 snapshot gRPC handler 运行日志。
	logger *slog.Logger
	// db 用于 snapshot 数据库健康探测。
	db *sql.DB
	// store 提供 overview、reliability 和 node inventory 的本地状态访问。
	store *store.Store
	// config 是当前 cloud-plane 进程从配置文件加载出的有效配置。
	config cloudplaneconfig.Config
	// auth 校验 control-plane southbound bearer token。
	auth controlplane.Authenticator
}

// NewServer 构造 snapshot southbound gRPC service。
// 参数说明：logger 记录该组件的结构化日志；db 用于健康探测；stores 提供本地状态访问；cfg 是有效配置；auth 校验 control-plane 内部 token。
func NewServer(logger *slog.Logger, db *sql.DB, stores *store.Store, cfg cloudplaneconfig.Config, auth controlplane.Authenticator) cloudplanev1.ControlPlaneSnapshotServiceServer {
	return &Server{logger: logger, db: db, store: stores, config: cfg, auth: auth}
}
