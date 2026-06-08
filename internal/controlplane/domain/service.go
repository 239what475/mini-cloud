package domain

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
	ErrPlaneIDRequired       = errors.New("planeID is required")
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
	PlaneID              string               `json:"planeID"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command"`
	Args                 []string             `json:"args"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env"`
	SecretEnv            map[string]string    `json:"secretEnv,omitempty"`
	RegistryCredentialID string               `json:"registryCredentialID"`
	Files                []projectedfile.File `json:"files,omitempty"`
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

type ServiceUpdateStatusInput struct {
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

type ServiceCreateInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

type ServiceUpdateInput struct {
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

func (in ServiceCreateInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return InvalidInput(ErrServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return InvalidInput(ErrInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return InvalidInput(ErrDisplayNameRequired)
	}
	return in.Spec.Validate()
}

func (in ServiceUpdateInput) Validate(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return InvalidInput(ErrServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(serviceName)) {
		return InvalidInput(ErrInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return InvalidInput(ErrDisplayNameRequired)
	}
	return in.Spec.Validate()
}

func (s Service) UpdateInput() ServiceUpdateInput {
	return ServiceUpdateInput{
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
		PlaneID:              input.PlaneID,
		InstanceClass:        input.InstanceClass,
		Exposure:             input.Exposure,
		Image:                input.Image,
		Command:              append([]string(nil), input.Command...),
		Args:                 append([]string(nil), input.Args...),
		DefaultPort:          input.DefaultPort,
		ReadinessPath:        input.ReadinessPath,
		Env:                  copyStringMap(input.Env),
		SecretEnv:            copyStringMap(input.SecretEnv),
		RegistryCredentialID: input.RegistryCredentialID,
		Files:                projectedfile.CloneFiles(input.Files),
	}
}

func (spec Spec) Validate() error {
	planeID, instanceClass, err := ResolveServicePlacementFields(spec.PlaneID, spec.InstanceClass)
	if err != nil {
		return InvalidInput(err)
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return InvalidInput(ErrInvalidExposure)
	}
	if strings.TrimSpace(spec.Image) == "" {
		return InvalidInput(ErrImageRequired)
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return InvalidInput(ErrInvalidDefaultPort)
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return InvalidInput(ErrInvalidReadinessPath)
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return InvalidInput(ErrInvalidEnvironmentKey)
		}
	}
	for key := range spec.SecretEnv {
		if strings.TrimSpace(key) == "" {
			return InvalidInput(ErrInvalidEnvironmentKey)
		}
	}
	if err := projectedfile.ValidateFiles(spec.Files); err != nil {
		return InvalidInput(err)
	}
	_ = planeID
	_ = instanceClass
	return nil
}

func serviceNeedsNewRun(before Spec, after Spec) bool {
	return !SpecRuntimeEqual(before, after)
}

func SpecRuntimeEqual(before Spec, after Spec) bool {
	return before.PlaneID == after.PlaneID &&
		before.InstanceClass == after.InstanceClass &&
		before.Image == after.Image &&
		reflect.DeepEqual(before.Command, after.Command) &&
		reflect.DeepEqual(before.Args, after.Args) &&
		reflect.DeepEqual(before.Env, after.Env) &&
		reflect.DeepEqual(before.SecretEnv, after.SecretEnv) &&
		before.DefaultPort == after.DefaultPort &&
		before.ReadinessPath == after.ReadinessPath &&
		before.RegistryCredentialID == after.RegistryCredentialID &&
		reflect.DeepEqual(projectedfile.CloneFiles(before.Files), projectedfile.CloneFiles(after.Files))
}

func ResolveServicePlacementFields(planeID string, instanceClass string) (string, string, error) {
	if strings.TrimSpace(planeID) == "" {
		return "", "", ErrPlaneIDRequired
	}
	if !IsInstanceClass(instanceClass) {
		return "", "", ErrInvalidInstanceClass
	}
	return strings.TrimSpace(planeID), instanceClass, nil
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
