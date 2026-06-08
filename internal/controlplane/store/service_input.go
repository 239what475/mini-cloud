package store

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	domain "mini-cloud/internal/controlplane/domain"
)

var (
	errServiceNameRequired      = errors.New("name is required")
	errInvalidServiceName       = errors.New("name must use lowercase letters, digits, and hyphens")
	errDisplayNameRequired      = errors.New("displayName is required")
	errInvalidExposure          = errors.New("exposure must be one of public, private")
	errImageRequired            = errors.New("image is required")
	errInvalidDefaultPort       = errors.New("defaultPort must be between 1 and 65535")
	errInvalidReadinessPath     = errors.New("readinessPath must start with /")
	errInvalidEnvironmentKey    = errors.New("env keys must not be empty")
	errRegistryServerRequired   = errors.New("registryCredential.server is required")
	errRegistryUsernameRequired = errors.New("registryCredential.username is required")
	errRegistryPasswordRequired = errors.New("registryCredential.password is required")
	errPlaneIDRequired          = errors.New("planeID is required")
	errInvalidInstanceClass     = errors.New("instanceClass must be one of small, medium, large")
	serviceNamePattern          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type ServiceCreateInput struct {
	Name        string
	DisplayName string
	Spec        domain.Spec
}

type ServiceUpdateInput struct {
	DisplayName string
	Spec        domain.Spec
}

type ServiceUpdateStatusInput struct {
	ObservedGeneration int64
	Phase              string
	Healthy            bool
	Message            string
	LastReconciledAt   *time.Time
	Run                *domain.RunStatus
	AssignedPlaneID    *string
	RemoteStatus       *string
	RemoteMessage      *string
}

func (in ServiceCreateInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return invalidInput(errInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	return validateServiceSpec(in.Spec)
}

func (in ServiceUpdateInput) validate(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(serviceName)) {
		return invalidInput(errInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	return validateServiceSpec(in.Spec)
}

func validateServiceSpec(spec domain.Spec) error {
	if _, _, err := resolveServicePlacementFields(spec.PlaneID, spec.InstanceClass); err != nil {
		return invalidInput(err)
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return invalidInput(errInvalidExposure)
	}
	if strings.TrimSpace(spec.Image) == "" {
		return invalidInput(errImageRequired)
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return invalidInput(errInvalidDefaultPort)
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return invalidInput(errInvalidReadinessPath)
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return invalidInput(errInvalidEnvironmentKey)
		}
	}
	for key := range spec.SecretEnv {
		if strings.TrimSpace(key) == "" {
			return invalidInput(errInvalidEnvironmentKey)
		}
	}
	if err := projectedfile.ValidateFiles(spec.Files); err != nil {
		return invalidInput(err)
	}
	if err := validateRegistryCredential(spec.RegistryCredential); err != nil {
		return invalidInput(err)
	}
	return nil
}

func validateRegistryCredential(input *domain.RegistryCredential) error {
	if input == nil {
		return nil
	}
	if strings.TrimSpace(input.Server) == "" {
		return errRegistryServerRequired
	}
	if strings.TrimSpace(input.Username) == "" {
		return errRegistryUsernameRequired
	}
	if strings.TrimSpace(input.Password) == "" {
		return errRegistryPasswordRequired
	}
	return nil
}

func resolveServicePlacementFields(planeID string, instanceClass string) (string, string, error) {
	if strings.TrimSpace(planeID) == "" {
		return "", "", errPlaneIDRequired
	}
	if !domain.IsInstanceClass(instanceClass) {
		return "", "", errInvalidInstanceClass
	}
	return strings.TrimSpace(planeID), instanceClass, nil
}
