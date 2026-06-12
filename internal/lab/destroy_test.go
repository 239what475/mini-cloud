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
