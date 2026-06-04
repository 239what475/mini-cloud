package resource

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrConfigSetNameRequired              = errors.New("config set name is required")
	ErrConfigSetNameInvalid               = errors.New("config set name must use lowercase letters, digits, and hyphens")
	ErrConfigSetValuesRequired            = errors.New("config set values must contain at least one item")
	ErrConfigSetValueKeyInvalid           = errors.New("config keys must not be empty")
	ErrSecretSetNameRequired              = errors.New("secret set name is required")
	ErrSecretSetNameInvalid               = errors.New("secret set name must use lowercase letters, digits, and hyphens")
	ErrSecretSetValuesRequired            = errors.New("secret set values must contain at least one item")
	ErrSecretSetValueKeyInvalid           = errors.New("secret keys must not be empty")
	ErrRegistryCredentialNameRequired     = errors.New("registry credential name is required")
	ErrRegistryCredentialNameInvalid      = errors.New("registry credential name must use lowercase letters, digits, and hyphens")
	ErrRegistryCredentialServerRequired   = errors.New("registry server is required")
	ErrRegistryCredentialUsernameRequired = errors.New("registry username is required")
	ErrRegistryCredentialPasswordRequired = errors.New("registry password is required")
	namePattern                           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type ConfigSet struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Values    map[string]string `json:"values"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type CreateConfigSetInput struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

func (in CreateConfigSetInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrConfigSetNameRequired
	}
	if !namePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrConfigSetNameInvalid
	}
	if len(in.Values) == 0 {
		return ErrConfigSetValuesRequired
	}
	for key := range in.Values {
		if strings.TrimSpace(key) == "" {
			return ErrConfigSetValueKeyInvalid
		}
	}
	return nil
}

type SecretSet struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Keys      []string          `json:"keys"`
	Values    map[string]string `json:"values,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type CreateSecretSetInput struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

func (in CreateSecretSetInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrSecretSetNameRequired
	}
	if !namePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrSecretSetNameInvalid
	}
	if len(in.Values) == 0 {
		return ErrSecretSetValuesRequired
	}
	for key := range in.Values {
		if strings.TrimSpace(key) == "" {
			return ErrSecretSetValueKeyInvalid
		}
	}
	return nil
}

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

func SecretKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
