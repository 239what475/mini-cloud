package workload

import (
	"errors"
	"testing"
)

func TestValidateFilesRejectsDuplicateMountPathAfterNormalization(t *testing.T) {
	t.Parallel()

	err := ValidateProjectedFiles([]ProjectedFile{
		{
			MountPath: "/etc/app/../app/config.yaml",
			Content:   "config",
		},
		{
			MountPath: "/etc/app/config.yaml",
			Content:   "secret",
			Sensitive: true,
		},
	})
	if !errors.Is(err, ErrDuplicateMountPath) {
		t.Fatalf("ValidateProjectedFiles error = %v, want ErrDuplicateMountPath", err)
	}
}

func TestCloneFilesAppliesDefaultModeBySensitivity(t *testing.T) {
	t.Parallel()

	files := CloneProjectedFiles([]ProjectedFile{
		{MountPath: "/etc/app/config.yaml", Content: "port: 8080"},
		{MountPath: "/etc/app/token", Content: "secret", Sensitive: true},
	})
	if len(files) != 2 {
		t.Fatalf("CloneProjectedFiles len = %d, want 2", len(files))
	}
	if files[0].Mode != DefaultConfigFileMode {
		t.Fatalf("config mode = %#o, want %#o", files[0].Mode, DefaultConfigFileMode)
	}
	if files[1].Mode != DefaultSecretFileMode {
		t.Fatalf("secret mode = %#o, want %#o", files[1].Mode, DefaultSecretFileMode)
	}
}

func TestFileValidateRejectsNonAbsoluteMountPath(t *testing.T) {
	t.Parallel()

	err := ProjectedFile{
		MountPath: "etc/app/config.yaml",
		Content:   "demo",
	}.Validate()
	if !errors.Is(err, ErrMountPathAbsolute) {
		t.Fatalf("ProjectedFile.Validate error = %v, want ErrMountPathAbsolute", err)
	}
}
