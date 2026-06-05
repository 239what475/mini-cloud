package operatorapi

import (
	"errors"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	controlservice "mini-cloud/internal/controlplane/service"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"
	"strings"
)

var errServiceSpecRequired = errors.New("spec is required")

func createInputFromProto(req *controlplanev1.CreateServiceRequest) (controlservice.CreateInput, error) {
	if req == nil || req.GetSpec() == nil {
		return controlservice.CreateInput{}, errServiceSpecRequired
	}
	spec := req.GetSpec()

	return controlservice.CreateInput{
		Name:        strings.TrimSpace(req.GetName()),
		DisplayName: strings.TrimSpace(req.GetDisplayName()),
		Spec: controlservice.Spec{
			Provider:             strings.TrimSpace(spec.GetProvider()),
			Region:               strings.TrimSpace(spec.GetRegion()),
			PinnedPlaneID:        strings.TrimSpace(spec.GetPinnedPlaneId()),
			Replicas:             int(spec.GetReplicas()),
			InstanceClass:        strings.TrimSpace(spec.GetInstanceClass()),
			Exposure:             strings.TrimSpace(spec.GetExposure()),
			Image:                strings.TrimSpace(spec.GetImage()),
			Command:              append([]string(nil), spec.GetCommand()...),
			Args:                 append([]string(nil), spec.GetArgs()...),
			DefaultPort:          int(spec.GetDefaultPort()),
			ReadinessPath:        strings.TrimSpace(spec.GetReadinessPath()),
			Env:                  copyStringMap(spec.GetEnv()),
			ConfigSetID:          strings.TrimSpace(spec.GetConfigSetId()),
			SecretSetID:          strings.TrimSpace(spec.GetSecretSetId()),
			RegistryCredentialID: strings.TrimSpace(spec.GetRegistryCredentialId()),
			ProjectedFiles:       projectedFilesFromProto(spec.GetProjectedFiles()),
			PersistentDirs:       persistentDirsFromProto(spec.GetPersistentDirs()),
		},
	}, nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func projectedFilesFromProto(items []*controlplanev1.ProjectedFileSpec) []projectedfile.Spec {
	if len(items) == 0 {
		return nil
	}
	out := make([]projectedfile.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, projectedfile.Spec{
			MountPath:  strings.TrimSpace(item.GetMountPath()),
			SourceKind: (strings.TrimSpace(item.GetSourceKind())),
			SourceID:   strings.TrimSpace(item.GetSourceId()),
			SourceKey:  strings.TrimSpace(item.GetSourceKey()),
		})
	}
	return projectedfile.CloneSpecs(out)
}

func persistentDirsFromProto(items []*controlplanev1.PersistentDirSpec) []persistentdir.Spec {
	if len(items) == 0 {
		return nil
	}
	out := make([]persistentdir.Spec, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, persistentdir.Spec{
			Name:      strings.TrimSpace(item.GetName()),
			MountPath: strings.TrimSpace(item.GetMountPath()),
		})
	}
	return persistentdir.CloneSpecs(out)
}
