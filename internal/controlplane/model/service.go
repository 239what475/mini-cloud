package model

import "time"

const (
	InstanceClassSmall  = "small"
	InstanceClassMedium = "medium"
	InstanceClassLarge  = "large"
)

const (
	ExposurePublic  = "public"
	ExposurePrivate = "private"
)

const (
	PhasePending     = "pending"
	PhaseProgressing = "progressing"
	PhaseReady       = "ready"
	PhaseDegraded    = "degraded"
	PhaseDeleting    = "deleting"
)

type Service struct {
	Metadata ServiceMetadata
	Spec     ServiceSpec
	Status   ServiceStatus
}

type ServiceMetadata struct {
	ID          string
	Name        string
	DisplayName string
	Host        string
	Generation  int64
}

type ServiceSpec struct {
	PlaneID       string
	InstanceClass string
	Exposure      string
	Image         string
	Command       []string
	Args          []string
	DefaultPort   int
	ReadinessPath string
	Env           map[string]string
}

type WorkloadSpec struct {
	InstanceClass string
	Exposure      string
	Image         string
	Command       []string
	Args          []string
	DefaultPort   int
	ReadinessPath string
	Env           map[string]string
}

type ServiceStatus struct {
	Observed  ServiceObservedStatus
	Run       RunStatus
	FrontDoor FrontDoorStatus
}

type ServiceObservedStatus struct {
	ObservedGeneration int64
	Phase              string
	Message            string
	LastObservedAt     *time.Time
}

const (
	RunPhasePending     = "pending"
	RunPhaseDispatching = "dispatching"
	RunPhaseRunning     = "running"
	RunPhaseFailed      = "failed"
)

type RunStatus struct {
	Phase   string
	Message string
}

type FrontDoorStatus struct {
	CNAME        string
	Verification *FrontDoorDNSRecord
}

type FrontDoorDNSRecord struct {
	Host       string
	RecordType string
	Value      string
}

func PendingRunStatus(message string) RunStatus {
	return RunStatus{
		Phase:   RunPhasePending,
		Message: message,
	}
}

func PendingServiceStatus(observedGeneration int64, message string) ServiceObservedStatus {
	return ServiceObservedStatus{
		ObservedGeneration: observedGeneration,
		Phase:              PhasePending,
		Message:            message,
	}
}

func DeletingServiceStatus(observedGeneration int64, message string) ServiceObservedStatus {
	return ServiceObservedStatus{
		ObservedGeneration: observedGeneration,
		Phase:              PhaseDeleting,
		Message:            message,
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

func IsServiceExposure(exposure string) bool {
	switch exposure {
	case ExposurePublic, ExposurePrivate:
		return true
	default:
		return false
	}
}

func IsServicePhase(phase string) bool {
	switch phase {
	case PhasePending, PhaseProgressing, PhaseReady, PhaseDegraded, PhaseDeleting:
		return true
	default:
		return false
	}
}

func IsRunPhase(phase string) bool {
	switch phase {
	case RunPhasePending, RunPhaseDispatching, RunPhaseRunning, RunPhaseFailed:
		return true
	default:
		return false
	}
}
