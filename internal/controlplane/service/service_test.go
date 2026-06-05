package service

import (
	"errors"
	"testing"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

func TestCreateInputValidateRejectsProjectedFileOverlapWithPersistentDir(t *testing.T) {
	t.Parallel()

	err := (CreateInput{
		Name:        "demo",
		DisplayName: "Demo",
		Spec: Spec{
			Provider:      "aliyun",
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
	}).Validate()
	if !errors.Is(err, persistentdir.ErrProjectedConflict) {
		t.Fatalf("Validate() error = %v, want %v", err, persistentdir.ErrProjectedConflict)
	}
}

func TestValidatePersistentDirUpdateRejectsRunChangingUpdateAfterRun(t *testing.T) {
	t.Parallel()

	current := serviceWithPersistentDir("ghcr.io/example/app:v1", false)
	current.Status.Run.CurrentRunID = "run-1"

	err := ValidatePersistentDirUpdate(current, updateWithPersistentDir("Demo", "aliyun", "cn-beijing", "", "ghcr.io/example/app:v2"))
	if !errors.Is(err, ErrPersistentDirsRunUpdateUnsupported) {
		t.Fatalf("ValidatePersistentDirUpdate() error = %v, want %v", err, ErrPersistentDirsRunUpdateUnsupported)
	}
}

func TestValidatePersistentDirUpdateAllowsNonRunChangeAfterRun(t *testing.T) {
	t.Parallel()

	current := serviceWithPersistentDir("ghcr.io/example/app:v1", false)
	current.Status.Run.CurrentRunID = "run-1"

	err := ValidatePersistentDirUpdate(current, updateWithPersistentDir("Demo v2", "aliyun", "cn-beijing", "", "ghcr.io/example/app:v1"))
	if err != nil {
		t.Fatalf("ValidatePersistentDirUpdate() returned error: %v", err)
	}
}

func TestValidatePersistentDirUpdateRejectsRunChangingUpdateWhenLockedWithoutRun(t *testing.T) {
	t.Parallel()

	current := serviceWithPersistentDir("ghcr.io/example/app:v1", true)

	err := ValidatePersistentDirUpdate(current, updateWithPersistentDir("Demo", "aliyun", "cn-beijing", "", "ghcr.io/example/app:v2"))
	if !errors.Is(err, ErrPersistentDirsRunUpdateUnsupported) {
		t.Fatalf("ValidatePersistentDirUpdate() error = %v, want %v", err, ErrPersistentDirsRunUpdateUnsupported)
	}
}

func TestValidatePersistentDirUpdateRejectsPlacementChangeWhenLockedWithoutRun(t *testing.T) {
	t.Parallel()

	current := serviceWithPersistentDir("ghcr.io/example/app:v1", true)
	current.Spec.PinnedPlaneID = "pln-001"

	err := ValidatePersistentDirUpdate(current, updateWithPersistentDir("Demo", "tencent", "ap-beijing", "pln-002", "ghcr.io/example/app:v1"))
	if !errors.Is(err, ErrPersistentDirsPlacementChangeUnsupported) {
		t.Fatalf("ValidatePersistentDirUpdate() error = %v, want %v", err, ErrPersistentDirsPlacementChangeUnsupported)
	}
}

func serviceWithPersistentDir(image string, locked bool) Service {
	return Service{
		Metadata: Metadata{DisplayName: "Demo"},
		Spec: Spec{
			Provider:             "aliyun",
			Region:               "cn-beijing",
			InstanceClass:        InstanceClassSmall,
			Exposure:             "public",
			Image:                image,
			DefaultPort:          8080,
			ReadinessPath:        "/healthz",
			PersistentDirsLocked: locked,
			PersistentDirs:       []persistentdir.Spec{{Name: "data", MountPath: "/var/lib/app"}},
		},
	}
}

func updateWithPersistentDir(displayName string, provider string, region string, pinnedPlaneID string, image string) UpdateInput {
	return UpdateInput{
		DisplayName: displayName,
		Spec: Spec{
			Provider:      provider,
			Region:        region,
			PinnedPlaneID: pinnedPlaneID,
			InstanceClass: InstanceClassSmall,
			Exposure:      "public",
			Image:         image,
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "data", MountPath: "/var/lib/app"},
			},
		},
	}
}
