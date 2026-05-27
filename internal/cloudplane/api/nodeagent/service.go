package nodeagent

import (
	"log/slog"
	"strings"
	"time"

	cloudplaneidentity "mini-cloud/internal/cloudplane/control/identity"
	"mini-cloud/internal/cloudplane/infra/store"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
)

// NewServer 构造 node-agent gRPC 服务实现。
// 参数说明：logger 记录该组件的结构化日志；stores 提供 cloud-plane 聚合状态访问；bootstrapToken 是 node-agent 注册或重新注册使用的 bootstrap token；sessionTTL 是 node-agent 会话 token 有效期。
func NewServer(logger *slog.Logger, stores *store.Store, bootstrapToken string, sessionTTL time.Duration) nodeagentv1.NodeAgentServiceServer {
	// sessionTTL 未配置或非法时使用保守默认值，避免签发立即过期的 node-agent 会话。
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}
	// bootstrapToken 只用于 RegisterNode 注册/重新注册；后续心跳、拉取任务、上报结果都使用会话 token。
	identityService := cloudplaneidentity.NewService(stores)
	return &service{
		logger:          logger,
		identityService: identityService,
		store:           stores,
		bootstrapToken:  strings.TrimSpace(bootstrapToken),
		sessionTTL:      sessionTTL,
	}
}

// service 实现 cloud-plane 暴露给 node-agent 的内部 gRPC 服务。
type service struct {
	// UnimplementedNodeAgentServiceServer 嵌入 proto 生成的前向兼容实现。
	nodeagentv1.UnimplementedNodeAgentServiceServer

	// logger 记录 node-agent gRPC handler 运行日志。
	logger *slog.Logger
	// identityService 负责 node-agent session token 的签发和解析。
	identityService *cloudplaneidentity.Service
	// store 提供 node-agent 注册、心跳、work claim 和 execution report 的事务边界。
	store *store.Store
	// bootstrapToken 是 node-agent 注册或重新注册时必须携带的 bearer token。
	bootstrapToken string
	// sessionTTL 是 node-agent 会话 token 有效期。
	sessionTTL time.Duration
}
