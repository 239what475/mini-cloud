package deploy

import (
	"errors"
	"testing"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

func TestApplyServiceInputResolvedSpecRejectsProjectedFileOverlapWithPersistentDir(t *testing.T) {
	t.Parallel()

	_, err := (ApplyServiceInput{
		Metadata: ServiceMetadata{
			ID:          "svc-1",
			Name:        "demo",
			DisplayName: "Demo",
			Generation:  1,
		},
		Spec: ServiceSpec{
			Region:        "cn-beijing",
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
	}).ResolvedSpec("")
	if !errors.Is(err, persistentdir.ErrProjectedConflict) {
		t.Fatalf("ResolvedSpec() error = %v, want %v", err, persistentdir.ErrProjectedConflict)
	}
}
