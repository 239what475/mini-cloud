package projectedfile

import (
	"errors"
	"testing"
)

func TestValidateSpecsRejectsDuplicateMountPathAfterNormalization(t *testing.T) {
	t.Parallel()

	err := ValidateSpecs([]Spec{
		{
			MountPath:  "/etc/app/../app/config.yaml",
			SourceKind: SourceKindConfigSet,
			SourceID:   "cfg_demo",
			SourceKey:  "config.yaml",
		},
		{
			MountPath:  "/etc/app/config.yaml",
			SourceKind: SourceKindSecretSet,
			SourceID:   "sec_demo",
			SourceKey:  "token",
		},
	})
	if !errors.Is(err, ErrDuplicateMountPath) {
		t.Fatalf("ValidateSpecs error = %v, want ErrDuplicateMountPath", err)
	}
}

func TestCloneFilesAppliesDefaultModeBySensitivity(t *testing.T) {
	t.Parallel()

	files := CloneFiles([]File{
		{MountPath: "/etc/app/config.yaml", Content: "port: 8080"},
		{MountPath: "/etc/app/token", Content: "secret", Sensitive: true},
	})
	if len(files) != 2 {
		t.Fatalf("CloneFiles len = %d, want 2", len(files))
	}
	if files[0].Mode != DefaultConfigMode {
		t.Fatalf("config mode = %#o, want %#o", files[0].Mode, DefaultConfigMode)
	}
	if files[1].Mode != DefaultSecretMode {
		t.Fatalf("secret mode = %#o, want %#o", files[1].Mode, DefaultSecretMode)
	}
}

func TestFileValidateRejectsNonAbsoluteMountPath(t *testing.T) {
	t.Parallel()

	err := File{
		MountPath: "etc/app/config.yaml",
		Content:   "demo",
	}.Validate()
	if !errors.Is(err, ErrMountPathAbsolute) {
		t.Fatalf("File.Validate error = %v, want ErrMountPathAbsolute", err)
	}
}
