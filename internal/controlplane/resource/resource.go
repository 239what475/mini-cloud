package resource

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrRegistryCredentialNameRequired     = errors.New("registry credential name is required")
	ErrRegistryCredentialNameInvalid      = errors.New("registry credential name must use lowercase letters, digits, and hyphens")
	ErrRegistryCredentialServerRequired   = errors.New("registry server is required")
	ErrRegistryCredentialUsernameRequired = errors.New("registry username is required")
	ErrRegistryCredentialPasswordRequired = errors.New("registry password is required")
	namePattern                           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type RegistryCredential struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Server             string    `json:"server"`
	Username           string    `json:"username"`
	Password           string    `json:"password,omitempty"`
	PasswordConfigured bool      `json:"passwordConfigured"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type CreateRegistryCredentialInput struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (in CreateRegistryCredentialInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrRegistryCredentialNameRequired
	}
	if !namePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrRegistryCredentialNameInvalid
	}
	if strings.TrimSpace(in.Server) == "" {
		return ErrRegistryCredentialServerRequired
	}
	if strings.TrimSpace(in.Username) == "" {
		return ErrRegistryCredentialUsernameRequired
	}
	if strings.TrimSpace(in.Password) == "" {
		return ErrRegistryCredentialPasswordRequired
	}
	return nil
}

func CloneValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
