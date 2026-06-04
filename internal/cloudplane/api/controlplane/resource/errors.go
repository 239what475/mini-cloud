package resource

import (
	"errors"

	"mini-cloud/internal/cloudplane/infra/store"
	"mini-cloud/internal/contract/cloudplaneapi"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

// applyResourcesStatusError 将全局资源 apply 错误转换为 gRPC 状态错误。
func (s *Server) applyResourcesStatusError(action string, err error) error {
	// 领域输入错误统一映射为 InvalidArgument。
	switch {
	case isConfigSetInputError(err),
		isSecretSetInputError(err),
		isRegistryCredentialInputError(err):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, store.ErrConfigSetNameAlreadyExists),
		errors.Is(err, store.ErrSecretSetNameAlreadyExists),
		errors.Is(err, store.ErrRegistryCredentialNameAlreadyExists):
		return status.Error(codes.Aborted, err.Error())
	default:
		// 其他错误记录内部日志，对调用方隐藏数据库或实现细节。
		s.logger.Error(action+" failed", "error", err)
		return status.Error(codes.Internal, action+" failed")
	}
}
