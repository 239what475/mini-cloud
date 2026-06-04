package deploy

import (
	"errors"
	"testing"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

func TestApplyServiceInputResolvedApplyRequestRejectsProjectedFileOverlapWithPersistentDir(t *testing.T) {
	t.Parallel()

		_, err := (ApplyServiceInput{
			Metadata: ServiceMetadata{
				ID:          "svc-1",
				Name:        "demo",
				DisplayName: "Demo",
			},
		Spec: ServiceSpec{
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/app:v1",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
			ProjectedFiles: []projectedfile.Spec{
				{MountPath: "/var/lib/app/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
			},
			PersistentDirs: []persistentdir.Spec{
				{Name: "data", MountPath: "/var/lib/app"},
			},
		},
	}).ResolvedApplyRequest("")
	if !errors.Is(err, persistentdir.ErrProjectedConflict) {
		t.Fatalf("ResolvedApplyRequest() error = %v, want %v", err, persistentdir.ErrProjectedConflict)
	}
}
