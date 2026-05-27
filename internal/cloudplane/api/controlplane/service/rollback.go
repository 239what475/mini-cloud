package service

import (
	"context"
	"errors"
	"strings"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/domain/desired"
	"mini-cloud/internal/cloudplane/domain/workload"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RollbackService 接受 control-plane 下发的 service rollback desired state。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；req 表示请求参数。
func (s *Server) RollbackService(ctx context.Context, req *cloudplanev1.RollbackServiceRequest) (*cloudplanev1.RollbackServiceResponse, error) {
	// rollback 会写入新的 desired state，必须先鉴权。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(req.GetProjectId())
	serviceID := strings.TrimSpace(req.GetServiceId())
	if projectID == "" || serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID and serviceID are required")
	}
	// revisionID 是要回滚到的历史 revision，不能为空。
	revisionID := strings.TrimSpace(req.GetRevisionId())
	if revisionID == "" {
		return nil, status.Error(codes.InvalidArgument, errRevisionIDRequired.Error())
	}

	// rollback 需要读取当前 service 和目标 revision，并把 revision 快照转换成新的 desired state。
	result, err := s.acceptRollbackDesired(ctx, projectID, serviceID, revisionID)
	if err != nil {
		return nil, s.rollbackStatusError(serviceID, revisionID, err)
	}

	// RollbackService 和 ApplyService 一样只确认 desired 已被接受；observed state 必须由 GetService 查询。
	return &cloudplanev1.RollbackServiceResponse{
		Action:            result.Action,
		DesiredGeneration: result.Desired.Generation,
	}, nil
}

// rollbackStatusError 把 rollback desired 错误转换成 gRPC 状态。
// 参数说明：serviceID 表示目标 service；revisionID 表示目标 revision；err 是 control 层返回的错误。
func (s *Server) rollbackStatusError(serviceID string, revisionID string, err error) error {
	switch {
	case errors.Is(err, errServiceIDRequired),
		errors.Is(err, errRevisionIDRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, errRevisionAlreadyCurrent):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, store.ErrServiceNotFound),
		errors.Is(err, store.ErrRevisionNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return s.acceptDesiredStatusError("accept service rollback", serviceID, revisionID, err)
	}
}

// acceptRollbackDesired 接受 rollback desired，并把目标 revision 快照转为新的 desired state。
// 参数说明：ctx 控制数据库请求生命周期；projectID/serviceID 定位目标 service；revisionID 是要恢复的历史 revision。
func (s *Server) acceptRollbackDesired(ctx context.Context, projectID string, serviceID string, revisionID string) (desired.AcceptResult, error) {
	// rollback 必须同时指定本地 serviceID 和目标 revisionID。
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return desired.AcceptResult{}, errServiceIDRequired
	}
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" {
		return desired.AcceptResult{}, errRevisionIDRequired
	}

	// 读取当前 service，用于确认 project 归属、当前 revision，以及 rollback 后仍应保留的规模/地域/暴露策略。
	serviceItem, err := s.store.GetService(ctx, serviceID)
	if err != nil {
		return desired.AcceptResult{}, err
	}
	// 请求中的 projectID 只用于防止跨 project 操作；不参与 revision 快照重建。
	if strings.TrimSpace(projectID) != "" && serviceItem.Metadata.ProjectID != strings.TrimSpace(projectID) {
		return desired.AcceptResult{}, store.ErrServiceNotFound
	}
	// 当前 revision 已经是目标 revision 时拒绝生成无意义 rollback desired。
	if serviceItem.Status.CurrentRevisionID == revisionID {
		return desired.AcceptResult{}, errRevisionAlreadyCurrent
	}

	// 目标 revision 必须属于该 service，避免跨 service revision 被误用。
	targetRevision, err := s.store.GetRevisionByService(ctx, serviceItem.Metadata.ID, revisionID)
	if err != nil {
		return desired.AcceptResult{}, err
	}

	// 用当前 service 的规模/地域/暴露策略，加上目标 revision 的运行规格，生成新的 desired。
	result, err := s.store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ProjectID:   serviceItem.Metadata.ProjectID,
		ServiceID:   serviceItem.Metadata.ID,
		Name:        serviceItem.Metadata.Name,
		DisplayName: serviceItem.Metadata.DisplayName,
		Spec: workload.Spec{
			Region:               serviceItem.Spec.Region,
			Replicas:             serviceItem.Spec.Replicas,
			InstanceClass:        serviceItem.Spec.InstanceClass,
			Exposure:             serviceItem.Spec.Exposure,
			Image:                targetRevision.Image,
			Command:              append([]string(nil), targetRevision.Command...),
			Args:                 append([]string(nil), targetRevision.Args...),
			DefaultPort:          targetRevision.Port,
			ReadinessPath:        targetRevision.ReadinessPath,
			Env:                  controlplane.CopyStringMap(targetRevision.Env),
			ConfigSetID:          targetRevision.ConfigSetID,
			SecretSetID:          targetRevision.SecretSetID,
			RegistryCredentialID: targetRevision.RegistryCredentialID,
			ProjectedFiles:       targetRevision.ProjectedFiles,
			PersistentDirs:       targetRevision.PersistentDirs,
		},
	})
	if err != nil {
		return desired.AcceptResult{}, err
	}

	return result, nil
}
