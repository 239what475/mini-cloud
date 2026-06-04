package service

import (
	"context"
	"errors"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DeleteService 接受 control-plane 下发的 service 删除请求，并在 cloud-plane 本地执行删除流程。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；req 表示请求参数。
func (s *Server) DeleteService(ctx context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	// 删除 service 会改变本地状态，必须先校验 control-plane 调用身份。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "serviceID is required")
	}

	// lifecycle mutation 自己按 serviceID 定位删除目标，API 层不再提前加载完整 service 对象。
	if err := s.lifecycle.Mutations.Delete(ctx, serviceID); err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			s.logger.Error("delete service failed", "service_id", serviceID, "error", err)
			return nil, status.Error(codes.Internal, "delete service failed")
		}
	}

	// 返回已删除标记和本地 serviceID；当前删除已在本地同步执行，不返回 service 详情。
	return &cloudplanev1.DeleteServiceResponse{
		Deleted:   true,
		ServiceId: serviceID,
	}, nil
}
