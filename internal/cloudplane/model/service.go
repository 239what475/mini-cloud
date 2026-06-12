package model

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	ServiceDesiredActive  = "active"
	ServiceDesiredDeleted = "deleted"
)

var serviceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Service struct {
	ID           string
	Name         string
	DisplayName  string
	Host         string
	Generation   int64
	DesiredState string
	Spec         ServiceSpec
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ServiceSpec struct {
	InstanceClass string
	Exposure      string
	Image         string
	Command       []string
	Args          []string
	Env           map[string]string
	ContainerPort int
	ReadinessPath string
}

type UpsertServiceInput struct {
	ID          string
	Name        string
	DisplayName string
	Host        string
	Generation  int64
	Spec        ServiceSpec
}

type DeleteServiceInput struct {
	ID         string
	Generation int64
}

func (in UpsertServiceInput) Validate() error {
	if strings.TrimSpace(in.ID) == "" {
		return errors.New("serviceID is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errors.New("serviceName is required")
	}
	if !serviceNamePattern.MatchString(name) {
		return errors.New("serviceName must use lowercase letters, digits, and hyphens")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return errors.New("displayName is required")
	}
	if strings.TrimSpace(in.Host) == "" {
		return errors.New("host is required")
	}
	if in.Generation <= 0 {
		return errors.New("serviceGeneration must be greater than 0")
	}
	return in.Spec.Validate()
}

func (in DeleteServiceInput) Validate() error {
	if strings.TrimSpace(in.ID) == "" {
		return errors.New("serviceID is required")
	}
	if in.Generation <= 0 {
		return errors.New("serviceGeneration must be greater than 0")
	}
	return nil
}

func (s ServiceSpec) Validate() error {
	if _, _, err := ResourceRequestForInstanceClass(s.InstanceClass); err != nil {
		return err
	}
	if _, err := ParseExposure(s.Exposure); err != nil {
		return err
	}
	if strings.TrimSpace(s.Image) == "" {
		return errors.New("image is required")
	}
	if s.ContainerPort <= 0 || s.ContainerPort > 65535 {
		return errors.New("containerPort must be between 1 and 65535")
	}
	readinessPath := strings.TrimSpace(s.ReadinessPath)
	if readinessPath == "" {
		return errors.New("readinessPath is required")
	}
	if !strings.HasPrefix(readinessPath, "/") {
		return errors.New("readinessPath must start with /")
	}
	for key := range s.Env {
		if strings.TrimSpace(key) == "" {
			return errors.New("env keys must not be empty")
		}
	}
	return nil
}
