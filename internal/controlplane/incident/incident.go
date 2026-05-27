package incident

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

const (
	SeverityWarning  = "warning"
	SeverityCritical = "critical"

	StateOpen     = "open"
	StateResolved = "resolved"
)

var (
	ErrPlaneIDRequired       = errors.New("planeID is required")
	ErrInvalidSeverity       = errors.New("severity must be one of warning, critical")
	ErrIncidentSummaryNeeded = errors.New("summary is required")
	ErrInvalidRunbookURL     = errors.New("runbookURL must be an absolute http or https URL")
)

type Incident struct {
	ID          string     `json:"id"`
	PlaneID     string     `json:"planeID"`
	Severity    string     `json:"severity"`
	State       string     `json:"state"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Resolution  string     `json:"resolution"`
	RunbookURL  string     `json:"runbookURL"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
}

type CreateInput struct {
	PlaneID     string `json:"planeID"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	RunbookURL  string `json:"runbookURL"`
}

type UpdateInput struct {
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	RunbookURL  string `json:"runbookURL"`
}

type ListFilter struct {
	PlaneID string
	State   string
	Limit   int
}

type ResolveInput struct {
	Resolution string `json:"resolution"`
}

func (in CreateInput) Validate() error {
	switch {
	case strings.TrimSpace(in.PlaneID) == "":
		return ErrPlaneIDRequired
	default:
		return validateMutableFields(in.Severity, in.Summary, in.RunbookURL)
	}
}

func (in CreateInput) ResolvedRunbookURL() (string, error) {
	return normalizeRunbookURL(in.RunbookURL)
}

func (in UpdateInput) Validate() error {
	return validateMutableFields(in.Severity, in.Summary, in.RunbookURL)
}

func (in UpdateInput) ResolvedRunbookURL() (string, error) {
	return normalizeRunbookURL(in.RunbookURL)
}

func IsSeverity(severity string) bool {
	switch severity {
	case SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

func IsState(state string) bool {
	switch state {
	case StateOpen, StateResolved:
		return true
	default:
		return false
	}
}

func validateMutableFields(severity string, summary string, runbookURL string) error {
	switch {
	case !IsSeverity(severity):
		return ErrInvalidSeverity
	case strings.TrimSpace(summary) == "":
		return ErrIncidentSummaryNeeded
	}

	if _, err := normalizeRunbookURL(runbookURL); err != nil {
		return err
	}
	return nil
}

func normalizeRunbookURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", ErrInvalidRunbookURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidRunbookURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
