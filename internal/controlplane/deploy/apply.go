package deploy

import (
	"errors"
	"regexp"
	"strings"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
)

var (
	ErrPlaneIDRequired       = errors.New("planeID is required")
	ErrServiceIDRequired     = errors.New("serviceID is required")
	ErrServiceNameRequired   = errors.New("name is required")
	ErrInvalidServiceName    = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired   = errors.New("displayName is required")
	ErrRegionRequired        = errors.New("region is required")
	ErrInvalidInstanceClass  = errors.New("instanceClass must be one of small, medium, large")
	ErrInvalidExposure       = errors.New("exposure must be one of public, private")
	ErrImageRequired         = errors.New("image is required")
	ErrInvalidDefaultPort    = errors.New("defaultPort must be between 1 and 65535")
	ErrInvalidReadinessPath  = errors.New("readinessPath must start with /")
	ErrInvalidEnvironmentKey = errors.New("env keys must not be empty")
	serviceNamePattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const (
	ApplyActionCreated = "created"
	ApplyActionUpdated = "updated"

	InstanceClassSmall  = "small"
	InstanceClassMedium = "medium"
	InstanceClassLarge  = "large"
)

type ApplyServiceInput struct {
	Metadata ServiceMetadata `json:"metadata"`
	Spec     ServiceSpec     `json:"spec"`
}

type ServiceMetadata struct {
	ID          string `json:"serviceID"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type ServiceSpec struct {
	Region               string               `json:"region"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command"`
	Args                 []string             `json:"args"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env"`
	ConfigSetID          string               `json:"configSetID"`
	SecretSetID          string               `json:"secretSetID"`
	RegistryCredentialID string               `json:"registryCredentialID"`
	ProjectedFiles       []projectedfile.Spec `json:"projectedFiles,omitempty"`
	PersistentDirs       []persistentdir.Spec `json:"persistentDirs,omitempty"`
}

type ApplyResult struct {
	PlaneID string `json:"planeID"`
	Action  string `json:"action"`
	PlanID  string `json:"planID"`
}

type DeleteServiceInput struct {
	ServiceID         string `json:"serviceID"`
	ServiceGeneration int64  `json:"serviceGeneration"`
	PlanID            string `json:"planID"`
}

func (in ApplyServiceInput) Validate() error {
	_, err := in.ResolvedSpec("")
	return err
}

func (in ApplyServiceInput) ResolvedSpec(defaultRegion string) (cloudplaneapi.ServiceSpec, error) {
	resolvedRegion := strings.TrimSpace(in.Spec.Region)
	if resolvedRegion == "" {
		resolvedRegion = strings.TrimSpace(defaultRegion)
	}

	if strings.TrimSpace(in.Metadata.ID) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrServiceIDRequired
	}
	if strings.TrimSpace(in.Metadata.Name) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Metadata.Name)) {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidServiceName
	}
	if strings.TrimSpace(in.Metadata.DisplayName) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrDisplayNameRequired
	}
	if strings.TrimSpace(resolvedRegion) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrRegionRequired
	}
	if !IsInstanceClass(in.Spec.InstanceClass) {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidInstanceClass
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(in.Spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidExposure
	}
	if strings.TrimSpace(in.Spec.Image) == "" {
		return cloudplaneapi.ServiceSpec{}, ErrImageRequired
	}
	if in.Spec.DefaultPort <= 0 || in.Spec.DefaultPort > 65535 {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidDefaultPort
	}
	if !strings.HasPrefix(strings.TrimSpace(in.Spec.ReadinessPath), "/") {
		return cloudplaneapi.ServiceSpec{}, ErrInvalidReadinessPath
	}
	for key := range in.Spec.Env {
		if strings.TrimSpace(key) == "" {
			return cloudplaneapi.ServiceSpec{}, ErrInvalidEnvironmentKey
		}
	}
	if err := projectedfile.ValidateSpecs(in.Spec.ProjectedFiles); err != nil {
		return cloudplaneapi.ServiceSpec{}, err
	}
	if err := persistentdir.ValidateContainerInputs(in.Spec.PersistentDirs, in.Spec.ProjectedFiles); err != nil {
		return cloudplaneapi.ServiceSpec{}, err
	}
	return cloudplaneapi.ServiceSpec{
		Region:               resolvedRegion,
		InstanceClass:        in.Spec.InstanceClass,
		Exposure:             resolvedExposure,
		Image:                in.Spec.Image,
		Command:              append([]string(nil), in.Spec.Command...),
		Args:                 append([]string(nil), in.Spec.Args...),
		DefaultPort:          in.Spec.DefaultPort,
		ReadinessPath:        strings.TrimSpace(in.Spec.ReadinessPath),
		Env:                  in.Spec.Env,
		ConfigSetID:          in.Spec.ConfigSetID,
		SecretSetID:          in.Spec.SecretSetID,
		RegistryCredentialID: in.Spec.RegistryCredentialID,
		ProjectedFiles:       projectedfile.CloneSpecs(in.Spec.ProjectedFiles),
		PersistentDirs:       persistentdir.CloneSpecs(in.Spec.PersistentDirs),
	}, nil
}

func IsInstanceClass(class string) bool {
	switch class {
	case InstanceClassSmall, InstanceClassMedium, InstanceClassLarge:
		return true
	default:
		return false
	}
}

func ResourceRequest(class string) (cpuMilli int, memoryMi int, err error) {
	switch class {
	case InstanceClassSmall:
		return 500, 512, nil
	case InstanceClassMedium:
		return 1000, 1024, nil
	case InstanceClassLarge:
		return 1500, 1536, nil
	default:
		return 0, 0, ErrInvalidInstanceClass
	}
}
