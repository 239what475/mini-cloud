package service

import (
	"context"
	"strings"

	"mini-cloud/internal/cloudplane/domain/desired"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ApplyService 接受 control-plane 下发的 service 期望状态，并持久化为 cloud-plane 本地期望。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；req 表示请求参数。
func (s *Server) ApplyService(ctx context.Context, req *cloudplanev1.ApplyServiceRequest) (*cloudplanev1.ApplyServiceResponse, error) {
	// 接受 desired state 会写入本地数据库，因此必须先做 southbound 鉴权。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	// serviceID 直接使用 control-plane 身份，cloud-plane 不再生成另一套 remote service 身份。
	serviceID := strings.TrimSpace(req.GetServiceId())
	serviceName := strings.TrimSpace(req.GetName())
	if serviceID == "" || serviceName == "" {
		return nil, status.Error(codes.InvalidArgument, "serviceID and name are required")
	}

	displayName := strings.TrimSpace(req.GetDisplayName())
	spec := workloadSpecFromProto(req.GetSpec())

	// ApplyService 只接受 service desired；UpsertServiceDesired 是本路径的事务边界：
	// 写入 accepted desired，并按输入变化生成新的 desired generation。
	result, err := s.store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ServiceID:   serviceID,
		Name:        serviceName,
		DisplayName: displayName,
		Spec:        spec,
	})
	if err != nil {
		return nil, s.acceptDesiredStatusError("apply service", serviceName, err)
	}
	// ApplyService 只确认 desired state 已被 cloud-plane 接受并持久化。
	// service 读模型和 observed status 必须由调用方通过 GetService 显式查询，避免把异步 reconcile 误表达成同步完成。
	return &cloudplanev1.ApplyServiceResponse{
		Action:            result.Action,
		DesiredGeneration: result.Desired.Generation,
	}, nil
}
