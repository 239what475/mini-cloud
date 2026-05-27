package plane

import (
	"errors"
	"testing"
)

func TestUpdateOperationInputValidate(t *testing.T) {
	input := UpdateOperationInput{
		State:  OperationStateMaintenance,
		Reason: "kernel upgrade",
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if input.ResolvedReason() != "kernel upgrade" {
		t.Fatalf("ResolvedReason = %q, want kernel upgrade", input.ResolvedReason())
	}
}

func TestUpdateOperationInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input UpdateOperationInput
		want  error
	}{
		{
			name:  "invalid state",
			input: UpdateOperationInput{State: "paused"},
			want:  ErrInvalidPlaneOperationState,
		},
		{
			name:  "missing reason when draining",
			input: UpdateOperationInput{State: OperationStateDraining},
			want:  ErrPlaneOperationReasonRequired,
		},
		{
			name:  "missing reason when maintenance",
			input: UpdateOperationInput{State: OperationStateMaintenance},
			want:  ErrPlaneOperationReasonRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOperationResolvedStateDefaultsToActive(t *testing.T) {
	var op Operation
	if op.ResolvedState() != OperationStateActive {
		t.Fatalf("ResolvedState = %v, want %v", op.ResolvedState(), OperationStateActive)
	}
	if !op.AcceptingNewDeployments() {
		t.Fatalf("AcceptingNewDeployments = false, want true")
	}
}
