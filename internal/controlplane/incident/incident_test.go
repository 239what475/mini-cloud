package incident

import (
	"errors"
	"testing"
)

func TestCreateInputValidate(t *testing.T) {
	input := CreateInput{
		PlaneID:     "pln_demo",
		Severity:    SeverityCritical,
		Summary:     "plane unreachable",
		Description: "heartbeat missing for 5 minutes",
		RunbookURL:  "https://runbooks.example.com/control/plane-offline",
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	runbookURL, err := input.ResolvedRunbookURL()
	if err != nil {
		t.Fatalf("ResolvedRunbookURL returned error: %v", err)
	}
	if runbookURL != "https://runbooks.example.com/control/plane-offline" {
		t.Fatalf("ResolvedRunbookURL = %q", runbookURL)
	}
}

func TestUpdateInputValidate(t *testing.T) {
	input := UpdateInput{
		Severity:    SeverityWarning,
		Summary:     "plane degraded",
		Description: "one worker is offline",
		RunbookURL:  "https://runbooks.example.com/control/plane-degraded",
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	runbookURL, err := input.ResolvedRunbookURL()
	if err != nil {
		t.Fatalf("ResolvedRunbookURL returned error: %v", err)
	}
	if runbookURL != "https://runbooks.example.com/control/plane-degraded" {
		t.Fatalf("ResolvedRunbookURL = %q", runbookURL)
	}
}

func TestCreateInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{
			name:  "missing plane id",
			input: CreateInput{Severity: SeverityWarning, Summary: "warn"},
			want:  ErrPlaneIDRequired,
		},
		{
			name:  "invalid severity",
			input: CreateInput{PlaneID: "pln_demo", Severity: "info", Summary: "warn"},
			want:  ErrInvalidSeverity,
		},
		{
			name:  "missing summary",
			input: CreateInput{PlaneID: "pln_demo", Severity: SeverityWarning},
			want:  ErrIncidentSummaryNeeded,
		},
		{
			name:  "invalid runbook url",
			input: CreateInput{PlaneID: "pln_demo", Severity: SeverityWarning, Summary: "warn", RunbookURL: "file:///tmp/runbook"},
			want:  ErrInvalidRunbookURL,
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

func TestUpdateInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input UpdateInput
		want  error
	}{
		{
			name:  "invalid severity",
			input: UpdateInput{Severity: "info", Summary: "warn"},
			want:  ErrInvalidSeverity,
		},
		{
			name:  "missing summary",
			input: UpdateInput{Severity: SeverityWarning},
			want:  ErrIncidentSummaryNeeded,
		},
		{
			name:  "invalid runbook url",
			input: UpdateInput{Severity: SeverityWarning, Summary: "warn", RunbookURL: "file:///tmp/runbook"},
			want:  ErrInvalidRunbookURL,
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
