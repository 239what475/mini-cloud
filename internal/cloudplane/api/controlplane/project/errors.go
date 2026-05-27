package project

import (
	"errors"

	"mini-cloud/internal/cloudplane/infra/store"
	commonproject "mini-cloud/internal/common/project"
	"mini-cloud/internal/contract/cloudplaneapi"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isProjectInputError 判断错误是否属于 project input error。
// 参数说明：err 是需要转换或包装的错误。
func isProjectInputError(err error) bool {
	return errors.Is(err, commonproject.ErrInvalidProjectName) ||
		errors.Is(err, commonproject.ErrDisplayNameRequired) ||
		errors.Is(err, commonproject.ErrOwnerUserIDRequired) ||
		errors.Is(err, commonproject.ErrQuotaMaxServicesInvalid) ||
		errors.Is(err, commonproject.ErrQuotaCPUMilliInvalid) ||
		errors.Is(err, commonproject.ErrQuotaMemoryMiInvalid)
}

// isConfigSetInputError 判断错误是否属于 config set input error。
// 参数说明：err 是需要转换或包装的错误。
func isConfigSetInputError(err error) bool {
	return errors.Is(err, cloudplaneapi.ErrConfigSetIDRequired) ||
		errors.Is(err, cloudplaneapi.ErrConfigSetNameRequired) ||
		errors.Is(err, cloudplaneapi.ErrConfigSetNameInvalid) ||
		errors.Is(err, cloudplaneapi.ErrConfigSetValuesRequired) ||
		errors.Is(err, cloudplaneapi.ErrConfigSetValueKeyInvalid)
}

// isSecretSetInputError 判断错误是否属于 secret set input error。
// 参数说明：err 是需要转换或包装的错误。
func isSecretSetInputError(err error) bool {
	return errors.Is(err, cloudplaneapi.ErrSecretSetIDRequired) ||
		errors.Is(err, cloudplaneapi.ErrSecretSetNameRequired) ||
		errors.Is(err, cloudplaneapi.ErrSecretSetNameInvalid) ||
		errors.Is(err, cloudplaneapi.ErrSecretSetValuesRequired) ||
		errors.Is(err, cloudplaneapi.ErrSecretSetValueKeyInvalid)
}

// isRegistryCredentialInputError 判断错误是否属于 registry credential input error。
// 参数说明：err 是需要转换或包装的错误。
func isRegistryCredentialInputError(err error) bool {
	return errors.Is(err, cloudplaneapi.ErrRegistryCredentialIDRequired) ||
		errors.Is(err, cloudplaneapi.ErrRegistryCredentialNameRequired) ||
		errors.Is(err, cloudplaneapi.ErrRegistryCredentialNameInvalid) ||
		errors.Is(err, cloudplaneapi.ErrRegistryCredentialServerRequired) ||
		errors.Is(err, cloudplaneapi.ErrRegistryCredentialUsernameRequired) ||
		errors.Is(err, cloudplaneapi.ErrRegistryCredentialPasswordRequired)
}

// applyProjectStatusError 将 project 聚合 apply 错误转换为 gRPC 状态错误。
// 参数说明：action 是当前操作名称；projectID 表示 project 的唯一标识；err 是需要转换的错误。
func (s *Server) applyProjectStatusError(action string, projectID string, err error) error {
	// 领域输入错误统一映射为 InvalidArgument。
	switch {
	case isProjectInputError(err),
		isConfigSetInputError(err),
		isSecretSetInputError(err),
		isRegistryCredentialInputError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	// project 名称不可变和 project resource 名称冲突都表示调用方提交了和既有状态冲突的输入。
	case errors.Is(err, errProjectNameImmutable),
		errors.Is(err, store.ErrConfigSetNameAlreadyExists),
		errors.Is(err, store.ErrSecretSetNameAlreadyExists),
		errors.Is(err, store.ErrRegistryCredentialNameAlreadyExists):
		return status.Error(codes.Aborted, err.Error())
	// apply project 理论上会先创建 project；如果后续 resource 仍遇到 project missing，说明输入归属无效或并发删除。
	case errors.Is(err, store.ErrProjectNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		// 其他错误记录内部日志，对调用方隐藏数据库或实现细节。
		s.logger.Error(action+" failed", "project_id", projectID, "error", err)
		return status.Error(codes.Internal, action+" failed")
	}
}
