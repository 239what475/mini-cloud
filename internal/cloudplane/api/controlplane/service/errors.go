package service

import (
	"errors"

	"mini-cloud/internal/cloudplane/domain/usage"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// acceptDesiredStatusError 把 desired accept 阶段的领域错误转换成 gRPC 状态。
func (s *Server) acceptDesiredStatusError(action string, serviceName string, err error) error {
	// guardrailErr 用 errors.As 提取，以便返回结构化 gRPC details。
	var guardrailErr *usage.QuotaExceededError
	// workload 输入错误说明请求 spec 不合法，映射为 InvalidArgument。
	switch {
	case errors.Is(err, errServiceIDRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	case workload.IsInputError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	// 引用的 config/secret/registry credential 不存在时返回 NotFound。
	case isDesiredReferenceNotFound(err):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, store.ErrServiceNameAlreadyExists):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, workload.ErrServiceIdentityConflict):
		return status.Error(codes.Aborted, err.Error())
	// resource guardrail 拒绝使用 Aborted，并附带每个维度的拒绝原因。
	case errors.As(err, &guardrailErr):
		return guardrailAdmissionStatusError(guardrailErr)
	default:
		// 未分类错误记录内部上下文，返回通用 Internal。
		s.logger.Error(action+" failed", "service_name", serviceName, "error", err)
		return status.Error(codes.Internal, action+" failed")
	}
}

// protoRejectReasons 将 resource guardrail 拒绝原因列表转换为 protobuf details。
// 参数说明：items 是 usage 领域计算出的 resource guardrail 拒绝原因集合。
func protoRejectReasons(items []usage.RejectReason) []*cloudplanev1.QuotaRejectReason {
	// 预分配输出切片容量，保持拒绝原因顺序与 usage 计算结果一致。
	out := make([]*cloudplanev1.QuotaRejectReason, 0, len(items))
	for _, item := range items {
		// 数值字段只做 int 到 int32 的 protobuf 类型转换，不改变 resource guardrail 计算结果。
		out = append(out, &cloudplanev1.QuotaRejectReason{
			Code:           string(item.Code),
			Message:        item.Message,
			Current:        int32(item.Current),
			RequestedDelta: int32(item.RequestedDelta),
			Projected:      int32(item.Projected),
			Limit:          int32(item.Limit),
		})
	}
	return out
}

// guardrailAdmissionStatusError 把 resource guardrail admission 错误转换为 gRPC 状态错误。
// 参数说明：guardrailErr 是 usage 领域返回的 resource guardrail 超限错误。
func guardrailAdmissionStatusError(guardrailErr *usage.QuotaExceededError) error {
	// 使用 Aborted 表示请求语义正确但因当前 resource guardrail 状态被拒绝。
	base := status.New(codes.Aborted, "resource guardrail admission rejected")
	// 把结构化拒绝原因放入 gRPC error details，方便 control-plane 精确展示超限维度。
	withDetails, err := base.WithDetails(&cloudplanev1.QuotaAdmissionRejected{
		RejectReasons: protoRejectReasons(guardrailErr.RejectReasons),
	})
	if err != nil {
		// details 构造失败时退回纯文本错误，避免因为错误详情序列化失败而丢失主错误。
		return status.Error(codes.Aborted, guardrailErr.Error())
	}
	// 返回携带 details 的 gRPC status error。
	return withDetails.Err()
}

var (
	// errServiceIDRequired 表示按本地 service 执行的 desired 操作缺少 serviceID。
	errServiceIDRequired = errors.New("serviceID is required")
)

// isDesiredReferenceNotFound 判断 desired accept 是否引用了不存在的 config/secret/registry credential。
// 参数说明：err 是 store 或领域层返回的错误。
func isDesiredReferenceNotFound(err error) bool {
	return errors.Is(err, store.ErrConfigSetNotFound) ||
		errors.Is(err, store.ErrSecretSetNotFound) ||
		errors.Is(err, store.ErrRegistryCredentialNotFound)
}

// getStatusError 将 service 查询错误转换成 gRPC 状态错误。
// 参数说明：action 是当前查询动作；serviceID 是目标 service；err 是底层错误。
func (s *Server) getStatusError(action string, serviceID string, err error) error {
	if errors.Is(err, store.ErrServiceNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	s.logger.Error(action+" failed", "service_id", serviceID, "error", err)
	return status.Error(codes.Internal, action+" failed")
}
