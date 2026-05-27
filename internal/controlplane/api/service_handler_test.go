package api

import (
	"testing"

	"mini-cloud/internal/common/persistentdir"
)

func TestIsServiceInputErrorTreatsPersistentDirValidationAsBadRequest(t *testing.T) {
	t.Parallel()

	if !isServiceInputError(persistentdir.ErrProjectedConflict) {
		t.Fatal("expected persistentdir projected conflict to be treated as service input error")
	}
}
