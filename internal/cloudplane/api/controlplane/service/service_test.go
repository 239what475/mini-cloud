package service

import (
	"testing"

	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

// TestWorkloadSpecFromProtoCarriesProjectedFilesAndPersistentDirs 验证 protobuf 转换保留 runtime input refs。
func TestWorkloadSpecFromProtoCarriesProjectedFilesAndPersistentDirs(t *testing.T) {
	t.Parallel()

	// 构造包含 projected files 和 persistent dirs 的 protobuf spec。
	spec := workloadSpecFromProto(&cloudplanev1.ServiceSpec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: "small",
		Exposure:      "public",
		Image:         "ghcr.io/example/service:v1",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
		ProjectedFiles: []*cloudplanev1.ProjectedFileSpec{
			{MountPath: "/etc/service/config.yaml", SourceKind: "config_set", SourceId: "cfg-1", SourceKey: "config.yaml"},
		},
		PersistentDirs: []*cloudplanev1.PersistentDirSpec{
			{Name: "data", MountPath: "/var/lib/service"},
		},
	})

	// workload spec 必须携带 projected file 和 persistent dir。
	if len(spec.ProjectedFiles) != 1 || spec.ProjectedFiles[0].MountPath != "/etc/service/config.yaml" {
		t.Fatalf("spec.ProjectedFiles = %+v", spec.ProjectedFiles)
	}
	if len(spec.PersistentDirs) != 1 || spec.PersistentDirs[0].MountPath != "/var/lib/service" {
		t.Fatalf("spec.PersistentDirs = %+v", spec.PersistentDirs)
	}
}
