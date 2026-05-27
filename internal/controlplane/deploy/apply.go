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
	ErrPlaneIDRequired            = errors.New("planeID is required")
	ErrProjectIDRequired          = errors.New("projectID is required")
	ErrServiceIDRequired          = errors.New("serviceID is required")
	ErrServiceNameRequired        = errors.New("name is required")
	ErrInvalidServiceName         = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired        = errors.New("displayName is required")
	ErrRegionRequired             = errors.New("region is required")
	ErrInvalidReplicas            = errors.New("replicas must be greater than 0")
	ErrInvalidInstanceClass       = errors.New("instanceClass must be one of small, medium, large")
	ErrInvalidExposure            = errors.New("exposure must be one of public, private")
	ErrImageRequired              = errors.New("image is required")
	ErrInvalidDefaultPort         = errors.New("defaultPort must be between 1 and 65535")
	ErrInvalidReadinessPath       = errors.New("readinessPath must start with /")
	ErrInvalidEnvironmentKey      = errors.New("env keys must not be empty")
	ErrPersistentDirsReplicaLimit = errors.New("persistentDirs currently require replicas to be exactly 1")
	serviceNamePattern            = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
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
	ProjectID   string `json:"-"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type ServiceSpec struct {
	Region               string               `json:"region"`
	Replicas             int                  `json:"replicas"`
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
	PlaneID           string `json:"planeID"`
	ProjectID         string `json:"projectID"`
	Action            string `json:"action"`
	DesiredGeneration int64  `json:"desiredGeneration"`
}

func (in ApplyServiceInput) Validate() error {
	if strings.TrimSpace(in.Metadata.ProjectID) == "" {
		return ErrProjectIDRequired
	}
	_, err := in.ResolvedApplyRequest("")
	return err
}

func (in ApplyServiceInput) ResolvedApplyRequest(defaultRegion string) (cloudplaneapi.ApplyServiceRequest, error) {
	resolvedRegion := strings.TrimSpace(in.Spec.Region)
	if resolvedRegion == "" {
		resolvedRegion = strings.TrimSpace(defaultRegion)
	}

	if strings.TrimSpace(in.Metadata.ID) == "" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrServiceIDRequired
	}
	if strings.TrimSpace(in.Metadata.Name) == "" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Metadata.Name)) {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidServiceName
	}
	if strings.TrimSpace(in.Metadata.DisplayName) == "" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrDisplayNameRequired
	}
	if strings.TrimSpace(resolvedRegion) == "" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrRegionRequired
	}
	if in.Spec.Replicas <= 0 {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidReplicas
	}
	if !IsInstanceClass(in.Spec.InstanceClass) {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidInstanceClass
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(in.Spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidExposure
	}
	if strings.TrimSpace(in.Spec.Image) == "" {
		return cloudplaneapi.ApplyServiceRequest{}, ErrImageRequired
	}
	if in.Spec.DefaultPort <= 0 || in.Spec.DefaultPort > 65535 {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidDefaultPort
	}
	if !strings.HasPrefix(strings.TrimSpace(in.Spec.ReadinessPath), "/") {
		return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidReadinessPath
	}
	for key := range in.Spec.Env {
		if strings.TrimSpace(key) == "" {
			return cloudplaneapi.ApplyServiceRequest{}, ErrInvalidEnvironmentKey
		}
	}
	if err := projectedfile.ValidateSpecs(in.Spec.ProjectedFiles); err != nil {
		return cloudplaneapi.ApplyServiceRequest{}, err
	}
	if err := persistentdir.ValidateContainerInputs(in.Spec.PersistentDirs, in.Spec.ProjectedFiles); err != nil {
		return cloudplaneapi.ApplyServiceRequest{}, err
	}
	if len(in.Spec.PersistentDirs) > 0 && in.Spec.Replicas != 1 {
		return cloudplaneapi.ApplyServiceRequest{}, ErrPersistentDirsReplicaLimit
	}

	return cloudplaneapi.ApplyServiceRequest{
		DisplayName: in.Metadata.DisplayName,
		Spec: cloudplaneapi.ServiceSpec{
			Region:               resolvedRegion,
			Replicas:             in.Spec.Replicas,
			InstanceClass:        in.Spec.InstanceClass,
			Exposure:             resolvedExposure,
			Image:                in.Spec.Image,
			Command:              in.Spec.Command,
			Args:                 in.Spec.Args,
			DefaultPort:          in.Spec.DefaultPort,
			ReadinessPath:        in.Spec.ReadinessPath,
			Env:                  in.Spec.Env,
			ConfigSetID:          in.Spec.ConfigSetID,
			SecretSetID:          in.Spec.SecretSetID,
			RegistryCredentialID: in.Spec.RegistryCredentialID,
			ProjectedFiles:       projectedfile.CloneSpecs(in.Spec.ProjectedFiles),
			PersistentDirs:       persistentdir.CloneSpecs(in.Spec.PersistentDirs),
		},
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
