package operatorapi

import (
	"testing"

	controlservice "mini-cloud/internal/controlplane/service"
)

func TestIsServiceInputErrorTreatsPersistentDirRunUpdateAsUserInput(t *testing.T) {
	t.Parallel()

	if !isServiceInputError(controlservice.ErrPersistentDirsRunUpdateUnsupported) {
		t.Fatal("expected persistentDirs run update limit to be treated as service input error")
	}
}
