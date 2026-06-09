package model

import (
	"time"

	"mini-cloud/internal/projectedfile"
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

type Service struct {
	Metadata  ServiceMetadata `json:"metadata"`
	Spec      ServiceSpec     `json:"spec"`
	Status    ServiceStatus   `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type ServiceMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type ServiceSpec struct {
	PlaneID            string                     `json:"planeID"`
	InstanceClass      string                     `json:"instanceClass"`
	Exposure           string                     `json:"exposure"`
	Image              string                     `json:"image"`
	Command            []string                   `json:"command"`
	Args               []string                   `json:"args"`
	DefaultPort        int                        `json:"defaultPort"`
	ReadinessPath      string                     `json:"readinessPath"`
	Env                map[string]string          `json:"env"`
	SecretEnv          map[string]string          `json:"secretEnv,omitempty"`
	RegistryCredential *ServiceRegistryCredential `json:"registryCredential,omitempty"`
	Files              []projectedfile.File       `json:"files,omitempty"`
}

type ServiceRegistryCredential struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

type ServiceStatus struct {
	DesiredState string                `json:"desiredState"`
	Observed     ServiceObservedStatus `json:"observed"`
	Run          RunStatus             `json:"run"`
}

type ServiceObservedStatus struct {
	ObservedGeneration int64      `json:"observedGeneration"`
	Phase              string     `json:"phase"`
	Healthy            bool       `json:"healthy"`
	Message            string     `json:"message,omitempty"`
	LastReconciledAt   *time.Time `json:"lastReconciledAt,omitempty"`
	AssignedPlaneID    string     `json:"assignedPlaneID,omitempty"`
	RemoteStatus       string     `json:"remoteStatus,omitempty"`
	RemoteMessage      string     `json:"remoteMessage,omitempty"`
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

func PendingServiceStatus(observedGeneration int64, message string) ServiceObservedStatus {
	return ServiceObservedStatus{
		ObservedGeneration: observedGeneration,
		Phase:              PhasePending,
		Healthy:            false,
		Message:            message,
	}
}

func DeletingServiceStatus(observedGeneration int64, message string) ServiceObservedStatus {
	return ServiceObservedStatus{
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

func CloneServiceSpec(input ServiceSpec) ServiceSpec {
	return ServiceSpec{
		PlaneID:            input.PlaneID,
		InstanceClass:      input.InstanceClass,
		Exposure:           input.Exposure,
		Image:              input.Image,
		Command:            append([]string(nil), input.Command...),
		Args:               append([]string(nil), input.Args...),
		DefaultPort:        input.DefaultPort,
		ReadinessPath:      input.ReadinessPath,
		Env:                copyStringMap(input.Env),
		SecretEnv:          copyStringMap(input.SecretEnv),
		RegistryCredential: CloneServiceRegistryCredential(input.RegistryCredential),
		Files:              projectedfile.CloneFiles(input.Files),
	}
}

func CloneServiceRegistryCredential(input *ServiceRegistryCredential) *ServiceRegistryCredential {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}

func IsInstanceClass(class string) bool {
	switch class {
	case InstanceClassSmall, InstanceClassMedium, InstanceClassLarge:
		return true
	default:
		return false
	}
}
