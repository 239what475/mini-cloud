package service

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
)

const (
	InstanceClassSmall  = "small"
	InstanceClassMedium = "medium"
	InstanceClassLarge  = "large"
)

const (
	DesiredStateActive  = "active"
	DesiredStateDeleted = "deleted"

	PhasePending     = "pending"
	PhaseProgressing = "progressing"
	PhaseReady       = "ready"
	PhaseDegraded    = "degraded"
	PhaseDeleting    = "deleting"
)

var (
	ErrServiceNameRequired   = errors.New("name is required")
	ErrInvalidServiceName    = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired   = errors.New("displayName is required")
	ErrInvalidExposure       = errors.New("exposure must be one of public, private")
	ErrImageRequired         = errors.New("image is required")
	ErrInvalidDefaultPort    = errors.New("defaultPort must be between 1 and 65535")
	ErrInvalidReadinessPath  = errors.New("readinessPath must start with /")
	ErrInvalidEnvironmentKey = errors.New("env keys must not be empty")
	ErrPinnedPlaneIDInvalid  = errors.New("pinnedPlaneID must not be blank when provided")
	ErrProviderRequired      = errors.New("provider is required")
	ErrRegionRequired        = errors.New("region is required")
	ErrInvalidInstanceClass  = errors.New("instanceClass must be one of small, medium, large")
	serviceNamePattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Service struct {
	Metadata  Metadata      `json:"metadata"`
	Spec      Spec          `json:"spec"`
	Status    ServiceStatus `json:"status"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type Metadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type Spec struct {
	Provider             string               `json:"provider"`
	Region               string               `json:"region"`
	PinnedPlaneID        string               `json:"pinnedPlaneID,omitempty"`
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
}

type ServiceStatus struct {
	DesiredState string    `json:"desiredState"`
	Observed     Status    `json:"observed"`
	Run          RunStatus `json:"run"`
}

type Status struct {
	ObservedGeneration int64      `json:"observedGeneration"`
	Phase              string     `json:"phase"`
	Healthy            bool       `json:"healthy"`
	Message            string     `json:"message,omitempty"`
	LastReconciledAt   *time.Time `json:"lastReconciledAt,omitempty"`
	AssignedPlaneID    string     `json:"assignedPlaneID,omitempty"`
	RemoteStatus       string     `json:"remoteStatus,omitempty"`
	RemoteMessage      string     `json:"remoteMessage,omitempty"`
}

type UpdateStatusInput struct {
	ObservedGeneration int64
	Phase              string
	Healthy            bool
	Message            string
	LastReconciledAt   *time.Time
	Run                *RunStatus
	AssignedPlaneID    *string
	RemoteStatus       *string
	RemoteMessage      *string
}

const (
	RunPhasePending     = "pending"
	RunPhaseDispatching = "dispatching"
	RunPhaseRunning     = "running"
	RunPhaseFailed      = "failed"
	RunPhaseSuperseded  = "superseded"
)

type RunStatus struct {
	CurrentRunID   string     `json:"currentRunID,omitempty"`
	LatestRunID    string     `json:"latestRunID,omitempty"`
	Phase          string     `json:"phase"`
	Message        string     `json:"message,omitempty"`
	LastObservedAt *time.Time `json:"lastObservedAt,omitempty"`
}

func CloneRunStatus(input RunStatus) RunStatus {
	out := input
	if input.LastObservedAt != nil {
		value := input.LastObservedAt.UTC()
		out.LastObservedAt = &value
	}
	return out
}

type CreateInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

type UpdateInput struct {
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

func (in CreateInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrInvalidServiceName
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return ErrDisplayNameRequired
	}
	return in.Spec.Validate()
}

func (in UpdateInput) Validate(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(serviceName)) {
		return ErrInvalidServiceName
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return ErrDisplayNameRequired
	}
	return in.Spec.Validate()
}

func (s Service) UpdateInput() UpdateInput {
	return UpdateInput{
		DisplayName: s.Metadata.DisplayName,
		Spec:        CloneSpec(s.Spec),
	}
}

func PendingStatus(observedGeneration int64, message string) Status {
	return Status{
		ObservedGeneration: observedGeneration,
		Phase:              PhasePending,
		Healthy:            false,
		Message:            message,
	}
}

func DeletingStatus(observedGeneration int64, message string) Status {
	return Status{
		ObservedGeneration: observedGeneration,
		Phase:              PhaseDeleting,
		Healthy:            false,
		Message:            message,
	}
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func CloneSpec(input Spec) Spec {
	return Spec{
		Provider:             input.Provider,
		Region:               input.Region,
		PinnedPlaneID:        input.PinnedPlaneID,
		InstanceClass:        input.InstanceClass,
		Exposure:             input.Exposure,
		Image:                input.Image,
		Command:              append([]string(nil), input.Command...),
		Args:                 append([]string(nil), input.Args...),
		DefaultPort:          input.DefaultPort,
		ReadinessPath:        input.ReadinessPath,
		Env:                  copyStringMap(input.Env),
		ConfigSetID:          input.ConfigSetID,
		SecretSetID:          input.SecretSetID,
		RegistryCredentialID: input.RegistryCredentialID,
		ProjectedFiles:       projectedfile.CloneSpecs(input.ProjectedFiles),
	}
}

func (spec Spec) Validate() error {
	provider, region, pinnedPlaneID, instanceClass, err := ResolveServiceAssignmentFields(spec.Provider, spec.Region, spec.PinnedPlaneID, spec.InstanceClass)
	if err != nil {
		return err
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return ErrInvalidExposure
	}
	if strings.TrimSpace(spec.Image) == "" {
		return ErrImageRequired
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return ErrInvalidDefaultPort
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return ErrInvalidReadinessPath
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidEnvironmentKey
		}
	}
	if err := projectedfile.ValidateSpecs(spec.ProjectedFiles); err != nil {
		return err
	}
	_ = provider
	_ = region
	_ = pinnedPlaneID
	_ = instanceClass
	return nil
}

func serviceNeedsNewRun(before Spec, after Spec) bool {
	return !SpecRuntimeEqual(before, after)
}

func SpecRuntimeEqual(before Spec, after Spec) bool {
	return before.Region == after.Region &&
		before.InstanceClass == after.InstanceClass &&
		before.Image == after.Image &&
		reflect.DeepEqual(before.Command, after.Command) &&
		reflect.DeepEqual(before.Args, after.Args) &&
		reflect.DeepEqual(before.Env, after.Env) &&
		before.DefaultPort == after.DefaultPort &&
		before.ReadinessPath == after.ReadinessPath &&
		before.ConfigSetID == after.ConfigSetID &&
		before.SecretSetID == after.SecretSetID &&
		before.RegistryCredentialID == after.RegistryCredentialID &&
		reflect.DeepEqual(projectedfile.CloneSpecs(before.ProjectedFiles), projectedfile.CloneSpecs(after.ProjectedFiles))
}

func ResolveServiceAssignmentFields(provider string, region string, pinnedPlaneID string, instanceClass string) (string, string, string, string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", "", "", "", ErrProviderRequired
	}
	if strings.TrimSpace(region) == "" {
		return "", "", "", "", ErrRegionRequired
	}
	if strings.TrimSpace(pinnedPlaneID) == "" {
		pinnedPlaneID = ""
	}
	if pinnedPlaneID != "" && strings.TrimSpace(pinnedPlaneID) == "" {
		return "", "", "", "", ErrPinnedPlaneIDInvalid
	}
	if !IsInstanceClass(instanceClass) {
		return "", "", "", "", ErrInvalidInstanceClass
	}
	return strings.TrimSpace(provider), strings.TrimSpace(region), strings.TrimSpace(pinnedPlaneID), instanceClass, nil
}

func NormalizeRunPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "":
		return RunPhasePending
	default:
		return strings.ToLower(strings.TrimSpace(phase))
	}
}

func IsInstanceClass(class string) bool {
	switch class {
	case InstanceClassSmall, InstanceClassMedium, InstanceClassLarge:
		return true
	default:
		return false
	}
}
