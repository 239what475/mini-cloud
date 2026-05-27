package project

import (
	"context"
	"errors"
	"strings"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/infra/store"
	commonproject "mini-cloud/internal/common/project"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ApplyProject 按 control-plane projectID 幂等同步本地 project 聚合。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；req 表示 project 及其下属资源的期望状态。
func (s *Server) ApplyProject(ctx context.Context, req *cloudplanev1.ApplyProjectRequest) (*cloudplanev1.ApplyProjectResponse, error) {
	// 未授权请求不能创建 project，也不能写入 config、secret 或镜像凭据。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	// projectID 直接使用 control-plane project ID；cloud-plane 不再生成另一套 remote project ID。
	projectID := strings.TrimSpace(req.GetProjectId())
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID is required")
	}
	// ownerUserID 是 project 归属模型必需字段，缺失时直接返回参数错误。
	ownerUserID := strings.TrimSpace(req.GetOwnerUserId())
	if ownerUserID == "" {
		return nil, status.Error(codes.InvalidArgument, "ownerUserID is required")
	}

	// gRPC handler 只负责把 protobuf 传输对象转换为两端共用的 project 聚合结构。
	project := projectFromProto(req)

	// applyProject 是 project 聚合的唯一写入口：先同步 project 元数据，再同步 project 下属资源。
	action, err := s.applyProject(ctx, project)
	if err != nil {
		return nil, s.applyProjectStatusError("apply project", projectID, err)
	}

	// 写接口只返回接受确认；project 详情如需读取必须走独立查询或快照。
	return &cloudplanev1.ApplyProjectResponse{
		Action:    action,
		ProjectId: projectID,
	}, nil
}

// projectFromProto 将 protobuf project apply request 转换为两端共用的 project 聚合结构。
// 参数说明：req 是 control-plane 通过 gRPC 下发的 project 期望状态。
func projectFromProto(req *cloudplanev1.ApplyProjectRequest) cloudplaneapi.Project {
	return cloudplaneapi.Project{
		ID:                  strings.TrimSpace(req.GetProjectId()),
		Name:                strings.TrimSpace(req.GetName()),
		DisplayName:         strings.TrimSpace(req.GetDisplayName()),
		OwnerUserID:         strings.TrimSpace(req.GetOwnerUserId()),
		Quota:               projectQuotaFromProto(req.GetQuota()),
		ConfigSets:          configSetsFromProto(req.GetConfigSets()),
		SecretSets:          secretSetsFromProto(req.GetSecretSets()),
		RegistryCredentials: registryCredentialsFromProto(req.GetRegistryCredentials()),
	}
}

// projectQuotaFromProto 将 protobuf project quota 转换为两端共用的 project quota。
// 参数说明：quota 是 control-plane 下发的 project quota；nil 时返回零值 quota。
func projectQuotaFromProto(quota *cloudplanev1.ProjectQuota) commonproject.Quota {
	if quota == nil {
		return commonproject.Quota{}
	}
	return commonproject.Quota{
		MaxServices: int(quota.GetMaxServices()),
		CPUMilli:    int(quota.GetCpuMilli()),
		MemoryMi:    int(quota.GetMemoryMi()),
	}
}

// configSetsFromProto 转换 project 下属 config set 期望状态。
// 参数说明：items 是 ApplyProjectRequest 携带的 config set 列表。
func configSetsFromProto(items []*cloudplanev1.ProjectConfigSet) []cloudplaneapi.ProjectConfigSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.ProjectConfigSet, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.ProjectConfigSet{
			ID:     strings.TrimSpace(item.GetId()),
			Name:   strings.TrimSpace(item.GetName()),
			Values: controlplane.CopyStringMap(item.GetValues()),
		})
	}
	return out
}

// secretSetsFromProto 转换 project 下属 secret set 期望状态。
// 参数说明：items 是 ApplyProjectRequest 携带的 secret set 列表。
func secretSetsFromProto(items []*cloudplanev1.ProjectSecretSet) []cloudplaneapi.ProjectSecretSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.ProjectSecretSet, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.ProjectSecretSet{
			ID:     strings.TrimSpace(item.GetId()),
			Name:   strings.TrimSpace(item.GetName()),
			Values: controlplane.CopyStringMap(item.GetValues()),
		})
	}
	return out
}

// registryCredentialsFromProto 转换 project 下属 registry credential 期望状态。
// 参数说明：items 是 ApplyProjectRequest 携带的 registry credential 列表。
func registryCredentialsFromProto(items []*cloudplanev1.ProjectRegistryCredential) []cloudplaneapi.ProjectRegistryCredential {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.ProjectRegistryCredential, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.ProjectRegistryCredential{
			ID:       strings.TrimSpace(item.GetId()),
			Name:     strings.TrimSpace(item.GetName()),
			Server:   strings.TrimSpace(item.GetServer()),
			Username: strings.TrimSpace(item.GetUsername()),
			Password: item.GetPassword(),
		})
	}
	return out
}

// errProjectNameImmutable 表示同一个 projectID 已绑定不同 name；cloud-plane 不允许通过 apply 重命名 project。
var errProjectNameImmutable = errors.New("project name is immutable")

const (
	// projectActionCreated 表示本次创建了新的本地 project。
	projectActionCreated = "created"
	// projectActionUpdated 表示本次更新了已有本地 project 或其下属资源。
	projectActionUpdated = "updated"
	// projectActionNoop 表示已有本地 project 聚合与输入一致，本次没有写入。
	projectActionNoop = "noop"
)

// applyProject 按 control-plane project 聚合期望状态更新本地状态。
// 参数说明：ctx 控制数据库请求生命周期；input 是 control-plane 下发的 project 聚合期望状态。
func (s *Server) applyProject(ctx context.Context, project cloudplaneapi.Project) (string, error) {
	// 先同步 project 元数据，后续下属资源依赖 project 外键存在。
	action, err := s.applyProjectMetadata(ctx, project)
	if err != nil {
		return "", err
	}

	// 再同步 project 下属资源。这里仍属于同一个 project 聚合 apply，不再暴露独立 resource apply 入口。
	resourcesChanged, err := s.store.ApplyProjectResources(ctx, project.ID, project)
	if err != nil {
		return "", err
	}
	if action == projectActionNoop && resourcesChanged {
		return projectActionUpdated, nil
	}
	return action, nil
}

// applyProjectMetadata 同步 project 自身元数据。
// 参数说明：ctx 控制数据库请求生命周期；ownerUserID 是已映射的本地 owner；input 是 project 聚合输入。
func (s *Server) applyProjectMetadata(ctx context.Context, project cloudplaneapi.Project) (string, error) {
	// projectID 直接来自 control-plane；存在时执行幂等更新，不存在时创建。
	current, err := s.store.GetProject(ctx, project.ID)
	if err == nil {
		// project name 是创建时确定的稳定名称；同一 projectID 改名会造成本地引用和审计语义漂移，直接拒绝。
		if current.Name != project.Name {
			return "", errProjectNameImmutable
		}
		if projectMetadataMatches(current, project) {
			return projectActionNoop, nil
		}
		// project 聚合字段统一更新；ownerUserID 直接保存 control-plane 下发的 owner 标识。
		_, err := s.store.UpdateProject(ctx, current.ID, commonproject.UpdateProjectInput{
			DisplayName: project.DisplayName,
			Quota:       project.Quota,
			OwnerUserID: project.OwnerUserID,
		})
		if err != nil {
			return "", err
		}
		return projectActionUpdated, nil
	}
	if !errors.Is(err, store.ErrProjectNotFound) {
		return "", err
	}

	// 本地尚无记录时，以 control-plane projectID 作为本地 ID 创建 project。
	_, err = s.store.CreateProject(ctx, commonproject.CreateProjectInput{
		ID:          project.ID,
		Name:        project.Name,
		DisplayName: project.DisplayName,
		OwnerUserID: project.OwnerUserID,
		Quota:       project.Quota,
	})
	if err != nil {
		return "", err
	}
	return projectActionCreated, nil
}

// projectMetadataMatches 判断已有本地 project 元数据是否已与 control-plane 输入一致。
// 参数说明：current 是本地 project；ownerUserID 是已映射出的本地 owner；input 是本次 apply 输入。
func projectMetadataMatches(current commonproject.Project, project cloudplaneapi.Project) bool {
	return current.Name == project.Name &&
		current.DisplayName == project.DisplayName &&
		current.OwnerUserID == project.OwnerUserID &&
		current.Quota.MaxServices == project.Quota.MaxServices &&
		current.Quota.CPUMilli == project.Quota.CPUMilli &&
		current.Quota.MemoryMi == project.Quota.MemoryMi
}
