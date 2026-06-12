package lab

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

func TestTerraformArgsUseAutoApprove(t *testing.T) {
	runner := Runner{}
	plane := Plane{Terraform: TerraformConfig{
		VarFile: "/tmp/mini-cloud.tfvars",
	}}

	applyArgs := runner.terraformApplyArgs(plane)
	if len(applyArgs) != 3 || applyArgs[0] != "apply" || applyArgs[1] != "-var-file=/tmp/mini-cloud.tfvars" || applyArgs[2] != "-auto-approve" {
		t.Fatalf("terraformApplyArgs = %+v", applyArgs)
	}

	destroyArgs := runner.terraformDestroyArgs(plane)
	if len(destroyArgs) != 3 || destroyArgs[0] != "destroy" || destroyArgs[1] != "-var-file=/tmp/mini-cloud.tfvars" || destroyArgs[2] != "-auto-approve" {
		t.Fatalf("terraformDestroyArgs = %+v", destroyArgs)
	}
}
