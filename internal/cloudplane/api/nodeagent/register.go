package nodeagent

import (
	"context"
	"time"

	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/contract/nodeagentapi"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RegisterNode 接受 node-agent 注册或重新注册，并签发后续 RPC 使用的 session token。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带 metadata；req 表示请求参数。
func (s *service) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	// 注册入口只能使用 bootstrap token，避免未注册节点直接访问 session token 保护的接口。
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, err
	}

	// 将 protobuf 请求转换为 contract 输入；contract 层负责基础字段校验。
	input := nodeagentapi.RegisterNodeRequest{
		Provider:      req.GetProvider(),
		Region:        req.GetRegion(),
		Name:          req.GetName(),
		PrivateIP:     req.GetPrivateIp(),
		PublicIP:      req.GetPublicIp(),
		InstanceID:    req.GetInstanceId(),
		InstanceType:  req.GetInstanceType(),
		CPUMilliTotal: int(req.GetCpuMilliTotal()),
		MemoryMiTotal: int(req.GetMemoryMiTotal()),
	}
	if err := input.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	registered, err := s.store.RegisterNode(ctx, node.RegisterInput{
		Provider:      input.Provider,
		Region:        input.Region,
		Name:          input.Name,
		PrivateIP:     input.PrivateIP,
		PublicIP:      input.PublicIP,
		InstanceID:    input.InstanceID,
		InstanceType:  input.InstanceType,
		CPUMilliTotal: input.CPUMilliTotal,
		MemoryMiTotal: input.MemoryMiTotal,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// 注册成功后签发绑定到 nodeID 的 session token；后续 RPC 都必须使用该 token。
	sessionToken, err := s.identityService.IssueNodeAgentSessionToken(ctx, registered.ID, s.sessionTTL)
	if err != nil {
		return nil, status.Error(codes.Internal, "issue node agent session token failed")
	}

	// 返回本地 nodeID、会话 token 和注册后 cloud-plane 记录的节点状态。
	return &nodeagentv1.RegisterNodeResponse{
		NodeId:         registered.ID,
		SessionToken:   sessionToken,
		ObservedStatus: registered.Status,
		AcceptedAt:     timestamppb.New(time.Now().UTC()),
	}, nil
}
