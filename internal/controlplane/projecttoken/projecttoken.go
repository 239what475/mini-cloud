package projecttoken

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNameRequired = errors.New("project api token name is required")
	ErrNameInvalid  = errors.New("project api token name must use lowercase letters, digits, and hyphens")

	namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Token struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectID"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
}

type IssueInput struct {
	Name string `json:"name"`
}

type IssuedToken struct {
	Token  Token  `json:"token"`
	Secret string `json:"secret"`
}

func (in IssueInput) Validate() error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrNameRequired
	}
	if !namePattern.MatchString(name) {
		return ErrNameInvalid
	}
	return nil
}
