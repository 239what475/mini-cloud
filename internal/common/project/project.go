package project

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidProjectName      = errors.New("project name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired     = errors.New("displayName is required")
	ErrOwnerUserIDRequired     = errors.New("ownerUserID is required")
	ErrQuotaMaxServicesInvalid = errors.New("quota.maxServices must be greater than 0")
	ErrQuotaCPUMilliInvalid    = errors.New("quota.cpuMilli must be greater than 0")
	ErrQuotaMemoryMiInvalid    = errors.New("quota.memoryMi must be greater than 0")
	projectNamePattern         = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const (
	DefaultQuotaMaxServices = 5
	DefaultQuotaCPUMilli    = 4000
	DefaultQuotaMemoryMi    = 8192
)

type Quota struct {
	MaxServices int `json:"maxServices"`
	CPUMilli    int `json:"cpuMilli"`
	MemoryMi    int `json:"memoryMi"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Quota       Quota     `json:"quota"`
	OwnerUserID string    `json:"ownerUserID"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CreateProjectInput struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Quota       Quota  `json:"quota,omitempty"`
	OwnerUserID string `json:"ownerUserID"`
}

type UpdateProjectInput struct {
	DisplayName string `json:"displayName"`
	Quota       Quota  `json:"quota,omitempty"`
	OwnerUserID string `json:"ownerUserID"`
}

func (in CreateProjectInput) Validate() error {
	if !projectNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrInvalidProjectName
	}
	if strings.TrimSpace(in.OwnerUserID) == "" {
		return ErrOwnerUserIDRequired
	}
	return validateMutableFields(in.DisplayName, in.Quota)
}

func (in UpdateProjectInput) Validate() error {
	if strings.TrimSpace(in.OwnerUserID) == "" {
		return ErrOwnerUserIDRequired
	}
	return validateMutableFields(in.DisplayName, in.Quota)
}

func validateMutableFields(displayName string, quota Quota) error {
	if strings.TrimSpace(displayName) == "" {
		return ErrDisplayNameRequired
	}
	if _, err := resolveQuota(quota); err != nil {
		return err
	}
	return nil
}

func DefaultQuota() Quota {
	return Quota{
		MaxServices: DefaultQuotaMaxServices,
		CPUMilli:    DefaultQuotaCPUMilli,
		MemoryMi:    DefaultQuotaMemoryMi,
	}
}

func (in CreateProjectInput) ResolveQuota() (Quota, error) {
	return resolveQuota(in.Quota)
}

func (in UpdateProjectInput) ResolveQuota() (Quota, error) {
	return resolveQuota(in.Quota)
}

func resolveQuota(input Quota) (Quota, error) {
	quota := DefaultQuota()
	if input.MaxServices != 0 {
		quota.MaxServices = input.MaxServices
	}
	if input.CPUMilli != 0 {
		quota.CPUMilli = input.CPUMilli
	}
	if input.MemoryMi != 0 {
		quota.MemoryMi = input.MemoryMi
	}

	switch {
	case quota.MaxServices <= 0:
		return Quota{}, ErrQuotaMaxServicesInvalid
	case quota.CPUMilli <= 0:
		return Quota{}, ErrQuotaCPUMilliInvalid
	case quota.MemoryMi <= 0:
		return Quota{}, ErrQuotaMemoryMiInvalid
	default:
		return quota, nil
	}
}
