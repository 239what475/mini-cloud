package ops

import (
	"errors"
	"testing"
)

func TestTerraformOutputUnavailable(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		errors.New("terraform output -json: exit status 1: No state file was found!"),
		errors.New("terraform output -json: exit status 1: The state file either has no outputs defined, or all the defined outputs are empty."),
		errors.New("terraform output -json: exit status 1: state has no outputs"),
	} {
		if !terraformOutputUnavailable(err) {
			t.Fatalf("terraformOutputUnavailable(%q) = false, want true", err)
		}
	}

	for _, err := range []error{
		nil,
		errors.New("terraform output -json: exit status 1: permission denied"),
		errors.New("parse terraform output: invalid character"),
	} {
		if terraformOutputUnavailable(err) {
			t.Fatalf("terraformOutputUnavailable(%v) = true, want false", err)
		}
	}
}

func TestControlPlaneImageRepo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		image string
		want  string
	}{
		{image: "ccr.ccs.tencentyun.com/example/mini-cloud-control-plane", want: "example/mini-cloud-control-plane"},
		{image: "ccr.ccs.tencentyun.com/example/mini-cloud-control-plane:e2e-20260615", want: "example/mini-cloud-control-plane"},
	} {
		got, err := controlPlaneImageRepo(tc.image)
		if err != nil {
			t.Fatalf("controlPlaneImageRepo(%q) returned error: %v", tc.image, err)
		}
		if got != tc.want {
			t.Fatalf("controlPlaneImageRepo(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestControlPlaneImageRepoRejectsInvalidReference(t *testing.T) {
	t.Parallel()

	for _, image := range []string{"control-plane", "https://example.com/control-plane"} {
		if _, err := controlPlaneImageRepo(image); err == nil {
			t.Fatalf("controlPlaneImageRepo(%q) returned nil error, want invalid reference error", image)
		}
	}
}

func TestIsControlPlaneE2ETag(t *testing.T) {
	t.Parallel()

	if !isControlPlaneE2ETag("e2e-20260615123802") {
		t.Fatalf("expected e2e tag to be recognized")
	}
	if isControlPlaneE2ETag("scf-amd64-20260614030051") {
		t.Fatalf("expected non-e2e tag to be ignored")
	}
}
