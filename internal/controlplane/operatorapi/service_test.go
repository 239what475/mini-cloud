package operatorapi

import (
	"testing"

	controlservice "mini-cloud/internal/controlplane/service"
)

func TestIsServiceInputErrorTreatsPersistentDirReplicaLimitAsUserInput(t *testing.T) {
	t.Parallel()

	if !isServiceInputError(controlservice.ErrPersistentDirsReplicaLimit) {
		t.Fatal("expected persistentDirs replica limit to be treated as service input error")
	}
}
