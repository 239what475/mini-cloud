package snapshot

import (
	"context"
	"errors"

	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// GetSnapshot 返回 cloud-plane 当前健康、容量、inventory 和可靠性聚合快照。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；空请求参数按接口签名保留，当前实现不读取。
func (s *Server) GetSnapshot(ctx context.Context, _ *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	// snapshot 属于 control-plane southbound 同步接口，读取前同样必须校验内部 bearer token。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	// 直接从 service 持有的真实依赖聚合本地状态；handler 只负责鉴权、错误映射和 protobuf 转换。
	item, err := s.collectSnapshot(ctx)
	if err != nil {
		if errors.Is(err, errDatabaseUnavailable) {
			return nil, status.Error(codes.Unavailable, "database ping failed")
		}
		s.logger.Error("build cloud-plane snapshot failed", "error", err)
		return nil, status.Error(codes.Internal, "build cloud-plane snapshot failed")
	}
	return protoSnapshot(item), nil
}
