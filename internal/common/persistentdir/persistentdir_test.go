package persistentdir

import (
	"errors"
	"testing"

	"mini-cloud/internal/common/projectedfile"
)

func TestValidateSpecs(t *testing.T) {
	t.Parallel()

	specs := []Spec{
		{Name: "auth-dir", MountPath: "/var/lib/app/auth"},
		{Name: "cache", MountPath: "/var/lib/app/cache"},
	}
	if err := ValidateSpecs(specs); err != nil {
		t.Fatalf("ValidateSpecs() error = %v", err)
	}
}

func TestValidateSpecsRejectsDuplicateName(t *testing.T) {
	t.Parallel()

	err := ValidateSpecs([]Spec{
		{Name: "auth-dir", MountPath: "/var/lib/app/auth"},
		{Name: "auth-dir", MountPath: "/var/lib/app/cache"},
	})
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("ValidateSpecs() error = %v, want %v", err, ErrDuplicateName)
	}
}

func TestValidateSpecsRejectsRootMount(t *testing.T) {
	t.Parallel()

	err := ValidateSpecs([]Spec{
		{Name: "auth-dir", MountPath: "/"},
	})
	if !errors.Is(err, ErrMountPathInvalid) {
		t.Fatalf("ValidateSpecs() error = %v, want %v", err, ErrMountPathInvalid)
	}
}

func TestMaterializeMounts(t *testing.T) {
	t.Parallel()

	mounts, err := MaterializeMounts("/data/persistent", "svc-123", []Spec{
		{Name: "auth-dir", MountPath: "/var/lib/app/auth"},
	})
	if err != nil {
		t.Fatalf("MaterializeMounts() error = %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("len(mounts) = %d, want 1", len(mounts))
	}
	if mounts[0].SourcePath != "/data/persistent/svc-123/auth-dir" {
		t.Fatalf("SourcePath = %q, want /data/persistent/svc-123/auth-dir", mounts[0].SourcePath)
	}
}

func TestValidateContainerInputsRejectsNestedDirs(t *testing.T) {
	t.Parallel()

	err := ValidateContainerInputs([]Spec{
		{Name: "data", MountPath: "/var/lib/app"},
		{Name: "auth", MountPath: "/var/lib/app/auth"},
	}, nil)
	if !errors.Is(err, ErrNestedMountPath) {
		t.Fatalf("ValidateContainerInputs() error = %v, want %v", err, ErrNestedMountPath)
	}
}

func TestValidateContainerInputsRejectsProjectedFileOverlap(t *testing.T) {
	t.Parallel()

	err := ValidateContainerInputs([]Spec{
		{Name: "data", MountPath: "/var/lib/app"},
	}, []projectedfile.Spec{
		{MountPath: "/var/lib/app/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
	})
	if !errors.Is(err, ErrProjectedConflict) {
		t.Fatalf("ValidateContainerInputs() error = %v, want %v", err, ErrProjectedConflict)
	}
}

func TestValidateContainerInputsRejectsPersistentDirNestedUnderProjectedFile(t *testing.T) {
	t.Parallel()

	err := ValidateContainerInputs([]Spec{
		{Name: "data", MountPath: "/etc/app/config.yaml/runtime"},
	}, []projectedfile.Spec{
		{MountPath: "/etc/app/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
	})
	if !errors.Is(err, ErrProjectedConflict) {
		t.Fatalf("ValidateContainerInputs() error = %v, want %v", err, ErrProjectedConflict)
	}
}
