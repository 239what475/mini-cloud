package lifecycle

import (
	"testing"

	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// TestSpecFromServiceRetainsProjectedFilesAndPersistentDirs 验证从 service 提取运行规格时保留挂载配置。
func TestSpecFromServiceRetainsProjectedFilesAndPersistentDirs(t *testing.T) {
	t.Parallel()

	// 构造已有 service 记录，包含需要在更新预览中保留的 projected file 和 persistent dir。
	spec := workload.SpecFromService(workload.Service{
		Spec: workload.Spec{
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: workload.InstanceClassSmall,
			Exposure:      workload.ExposurePublic,
			Image:         "ghcr.io/example/service:v1",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
			ProjectedFiles: []projectedfile.Spec{
				{MountPath: "/etc/service/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
			},
			PersistentDirs: []persistentdir.Spec{
				{Name: "data", MountPath: "/var/lib/service"},
			},
		},
	})

	// projected file 必须从 service 记录复制到 spec。
	if len(spec.ProjectedFiles) != 1 || spec.ProjectedFiles[0].MountPath != "/etc/service/config.yaml" {
		t.Fatalf("spec.ProjectedFiles = %+v", spec.ProjectedFiles)
	}
	// persistent dir 也必须保留，避免更新流程误判为删除持久目录。
	if len(spec.PersistentDirs) != 1 || spec.PersistentDirs[0].Name != "data" {
		t.Fatalf("spec.PersistentDirs = %+v", spec.PersistentDirs)
	}
}
