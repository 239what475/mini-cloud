package service

import (
	"strings"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

// workloadSpecFromProto 将 protobuf ServiceSpec 转换为 workload 运行规格。
// 参数说明：spec 是 control-plane 下发的 workload 规格；nil 时返回空规格，交由领域校验报错。
func workloadSpecFromProto(spec *cloudplanev1.ServiceSpec) workload.Spec {
	// protobuf optional message 可能为空；转换层不直接构造 gRPC 错误，只返回空值让后续校验处理。
	if spec == nil {
		return workload.Spec{}
	}
	// 部分标量字符串字段在边界层裁剪空白；Command、Args 和 Env 内容保持调用方原值。
	return workload.Spec{
		Region:        strings.TrimSpace(spec.GetRegion()),
		Replicas:      int(spec.GetReplicas()),
		InstanceClass: strings.TrimSpace(spec.GetInstanceClass()),
		Exposure:      strings.TrimSpace(spec.GetExposure()),
		Image:         strings.TrimSpace(spec.GetImage()),
		// Command 和 Args 复制后写入内部输入，避免共享 protobuf getter 返回的底层切片。
		Command:       append([]string(nil), spec.GetCommand()...),
		Args:          append([]string(nil), spec.GetArgs()...),
		DefaultPort:   int(spec.GetDefaultPort()),
		ReadinessPath: strings.TrimSpace(spec.GetReadinessPath()),
		// Env 复制为新 map；ProjectedFiles 按 MountPath 排序，PersistentDirs 按 Name/MountPath 排序。
		Env:                  controlplane.CopyStringMap(spec.GetEnv()),
		ConfigSetID:          strings.TrimSpace(spec.GetConfigSetId()),
		SecretSetID:          strings.TrimSpace(spec.GetSecretSetId()),
		RegistryCredentialID: strings.TrimSpace(spec.GetRegistryCredentialId()),
		ProjectedFiles:       projectedFilesFromProto(spec.GetProjectedFiles()),
		PersistentDirs:       persistentDirsFromProto(spec.GetPersistentDirs()),
	}
}

// projectedFilesFromProto 将 protobuf projected file spec 列表转换为领域输入。
// 参数说明：items 是 control-plane 下发的 projected file spec 列表。
func projectedFilesFromProto(items []*cloudplanev1.ProjectedFileSpec) []projectedfile.Spec {
	// 空列表保持为 nil，表示请求没有声明 projected files。
	if len(items) == 0 {
		return nil
	}
	// 预分配容量；nil 元素会被跳过，避免坏请求中的空 message 导致 panic。
	out := make([]projectedfile.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		// 每个字符串字段在边界层裁剪空白；合法性由 projectedfile.ValidateSpecs 负责。
		out = append(out, projectedfile.Spec{
			MountPath:  strings.TrimSpace(item.GetMountPath()),
			SourceKind: strings.TrimSpace(item.GetSourceKind()),
			SourceID:   strings.TrimSpace(item.GetSourceId()),
			SourceKey:  strings.TrimSpace(item.GetSourceKey()),
		})
	}
	// CloneSpecs 会生成独立副本，规范化字段，并按 MountPath 确定性排序。
	return projectedfile.CloneSpecs(out)
}

// persistentDirsFromProto 将 protobuf persistent dir spec 列表转换为领域输入。
// 参数说明：items 是 control-plane 下发的 persistent dir spec 列表。
func persistentDirsFromProto(items []*cloudplanev1.PersistentDirSpec) []persistentdir.Spec {
	// 空列表保持为 nil，表示请求没有声明 persistent dirs。
	if len(items) == 0 {
		return nil
	}
	// 预分配容量；nil 元素会被跳过，避免坏请求中的空 message 导致 panic。
	out := make([]persistentdir.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		// 名称和挂载路径先裁剪空白；合法性由 persistentdir 校验逻辑负责。
		out = append(out, persistentdir.Spec{
			Name:      strings.TrimSpace(item.GetName()),
			MountPath: strings.TrimSpace(item.GetMountPath()),
		})
	}
	// CloneSpecs 会生成独立副本，规范化字段，并按 Name、MountPath 确定性排序。
	return persistentdir.CloneSpecs(out)
}

// protoService 将已接受的 service desired/spec contract 视图转为 protobuf。
// 参数说明：item 是 cloud-plane 接受并持久化后的 service 视图。
func protoService(item cloudplaneapi.Service) *cloudplanev1.Service {
	// 这里仅做 accepted service 领域视图到 protobuf 的字段映射，不重新校验 service 规格。
	return &cloudplanev1.Service{
		Metadata: &cloudplanev1.ServiceMetadata{
			Id:          item.Metadata.ID,
			Name:        item.Metadata.Name,
			DisplayName: item.Metadata.DisplayName,
		},
		Spec: &cloudplanev1.ServiceSpec{
			Region:               item.Spec.Region,
			Replicas:             int32(item.Spec.Replicas),
			InstanceClass:        item.Spec.InstanceClass,
			Exposure:             item.Spec.Exposure,
			Image:                item.Spec.Image,
			Command:              append([]string(nil), item.Spec.Command...),
			Args:                 append([]string(nil), item.Spec.Args...),
			DefaultPort:          int32(item.Spec.DefaultPort),
			ReadinessPath:        item.Spec.ReadinessPath,
			Env:                  controlplane.CopyStringMap(item.Spec.Env),
			ConfigSetId:          item.Spec.ConfigSetID,
			SecretSetId:          item.Spec.SecretSetID,
			RegistryCredentialId: item.Spec.RegistryCredentialID,
			ProjectedFiles:       protoProjectedFiles(item.Spec.ProjectedFiles),
			PersistentDirs:       protoPersistentDirs(item.Spec.PersistentDirs),
		},
		Status: &cloudplanev1.ServiceStatus{
			Phase:               item.Status.Phase,
			CurrentRevisionId:   item.Status.CurrentRevisionID,
			CandidateRevisionId: item.Status.CandidateRevisionID,
			RolloutPhase:        item.Status.RolloutPhase,
			RolloutMessage:      item.Status.RolloutMessage,
		},
	}
}

// protoObservedServiceStatus 将 cloud-plane 观测到的 service 运行态和 rollout 状态转为 protobuf。
// 参数说明：item 是 service 当前观测状态的 contract 视图。
func protoObservedServiceStatus(item cloudplaneapi.ObservedServiceStatus) *cloudplanev1.ObservedServiceStatus {
	// 顶层状态描述 service 当前已知 current revision、健康判断和可读消息。
	return &cloudplanev1.ObservedServiceStatus{
		CurrentRevisionId: item.CurrentRevisionID,
		Healthy:           item.Healthy,
		Message:           item.Message,
		// Rollout 嵌套对象透传 stable/candidate 两套副本计数和 rollout 阶段。
		Rollout: &cloudplanev1.ObservedRolloutStatus{
			Phase:               item.Rollout.Phase,
			Message:             item.Rollout.Message,
			StableRevisionId:    item.Rollout.StableRevisionID,
			CandidateRevisionId: item.Rollout.CandidateRevisionID,
			// stable 副本计数描述当前承载流量 revision 的目标、ready 和 available 数量。
			StableDesiredReplicas:   int32(item.Rollout.StableDesiredReplicas),
			StableReadyReplicas:     int32(item.Rollout.StableReadyReplicas),
			StableAvailableReplicas: int32(item.Rollout.StableAvailableReplicas),
			// candidate 副本计数描述正在验证或等待提升 revision 的目标、ready 和 available 数量。
			CandidateDesiredReplicas:   int32(item.Rollout.CandidateDesiredReplicas),
			CandidateReadyReplicas:     int32(item.Rollout.CandidateReadyReplicas),
			CandidateAvailableReplicas: int32(item.Rollout.CandidateAvailableReplicas),
			// ObservedAt 直接使用上游 rollout 视图给出的观测时间。
			ObservedAt: controlplane.ProtoTimestamp(item.Rollout.ObservedAt),
		},
	}
}

// protoProjectedFiles 将 revision projected file spec 列表转换为 protobuf。
// 参数说明：items 是 revision 领域对象中保存的 projected file spec 集合。
func protoProjectedFiles(items []projectedfile.Spec) []*cloudplanev1.ProjectedFileSpec {
	// 空列表返回 nil。
	if len(items) == 0 {
		return nil
	}
	// CloneSpecs 会复制、规范化并按 MountPath 排序；输出顺序不是原始输入顺序。
	out := make([]*cloudplanev1.ProjectedFileSpec, 0, len(items))
	for _, item := range projectedfile.CloneSpecs(items) {
		// SourceKind 在领域中是枚举字符串，protobuf 字段使用 string 表达。
		out = append(out, &cloudplanev1.ProjectedFileSpec{
			MountPath:  item.MountPath,
			SourceKind: item.SourceKind,
			SourceId:   item.SourceID,
			SourceKey:  item.SourceKey,
		})
	}
	return out
}

// protoPersistentDirs 将 revision persistent dir spec 列表转换为 protobuf。
// 参数说明：items 是 revision 领域对象中保存的 persistent dir spec 集合。
func protoPersistentDirs(items []persistentdir.Spec) []*cloudplanev1.PersistentDirSpec {
	// 空列表返回 nil，表示该 revision 没有声明 persistent dirs。
	if len(items) == 0 {
		return nil
	}
	// CloneSpecs 会复制、规范化并按 Name、MountPath 排序；输出顺序不是原始输入顺序。
	out := make([]*cloudplanev1.PersistentDirSpec, 0, len(items))
	for _, item := range persistentdir.CloneSpecs(items) {
		// persistent dir 只包含名称和容器挂载路径，不携带宿主机路径。
		out = append(out, &cloudplanev1.PersistentDirSpec{
			Name:      item.Name,
			MountPath: item.MountPath,
		})
	}
	return out
}
