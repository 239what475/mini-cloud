package serviceaccount

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	PlatformRoleOwner    = "platform_owner"
	PlatformRoleAdmin    = "platform_admin"
	PlatformRoleOperator = "platform_operator"
	PlatformRoleAuditor  = "platform_auditor"
)

var (
	ErrNameRequired = errors.New("platform service account name is required")
	ErrNameInvalid  = errors.New("platform service account name must use lowercase letters, digits, and hyphens")
	ErrInvalidRole  = errors.New("invalid platform service account role")

	namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Account struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	TokenPrefix string     `json:"tokenPrefix"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

type IssueInput struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type IssuedAccount struct {
	Account Account `json:"account"`
	Secret  string  `json:"secret"`
}

func (in IssueInput) Validate() error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrNameRequired
	}
	if !namePattern.MatchString(name) {
		return ErrNameInvalid
	}
	if err := ValidateRole(in.Role); err != nil {
		return err
	}
	return nil
}

func ValidateRole(role string) error {
	switch role {
	case PlatformRoleOwner, PlatformRoleAdmin, PlatformRoleOperator, PlatformRoleAuditor:
		return nil
	default:
		return ErrInvalidRole
	}
}
