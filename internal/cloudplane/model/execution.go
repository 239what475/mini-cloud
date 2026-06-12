package model

import (
	"errors"
	"strings"
	"time"
)

const (
	StatusPending    = "pending"
	StatusDeploying  = "deploying"
	StatusRunning    = "running"
	StatusSucceeded  = "succeeded"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
)

const (
	WorkActionRun    = "run"
	WorkActionDelete = "delete"
)

const (
	ExposurePublic  = "public"
	ExposurePrivate = "private"
)

type PlanInput struct {
	PlanID            string
	ServiceID         string
	ServiceName       string
	ServiceGeneration int64
	Image             string
	Command           []string
	Args              []string
	Env               map[string]string
	ContainerPort     int
	ReadinessPath     string
	CPUMilliRequest   int
	MemoryMiRequest   int
	Exposure          string
}

type ExecutionSnapshot struct {
	PlanID            string
	ServiceID         string
	ServiceName       string
	ServiceGeneration int64
	Status            string
	LastStatusReason  string
	ObservedAt        time.Time
}

type DeletePlanInput struct {
	ServiceID         string
	ServiceGeneration int64
	PlanID            string
}

func (in DeletePlanInput) Validate() error {
	if strings.TrimSpace(in.ServiceID) == "" {
		return errors.New("serviceID is required")
	}
	if strings.TrimSpace(in.PlanID) == "" {
		return errors.New("planID is required")
	}
	if in.ServiceGeneration <= 0 {
		return errors.New("serviceGeneration must be greater than 0")
	}
	return nil
}

func (in PlanInput) Validate() error {
	if strings.TrimSpace(in.PlanID) == "" {
		return errors.New("planID is required")
	}
	if strings.TrimSpace(in.ServiceID) == "" {
		return errors.New("serviceID is required")
	}
	if strings.TrimSpace(in.ServiceName) == "" {
		return errors.New("serviceName is required")
	}
	if strings.TrimSpace(in.Image) == "" {
		return errors.New("image is required")
	}
	if in.ContainerPort <= 0 || in.ContainerPort > 65535 {
		return errors.New("containerPort must be between 1 and 65535")
	}
	if in.CPUMilliRequest <= 0 || in.MemoryMiRequest <= 0 {
		return errors.New("cpuMilliRequest and memoryMiRequest must be greater than 0")
	}
	if in.Exposure != ExposurePublic && in.Exposure != ExposurePrivate {
		return errors.New("exposure must be public or private")
	}
	return nil
}

func ResourceRequestForInstanceClass(class string) (int, int, error) {
	switch strings.TrimSpace(class) {
	case "", "small":
		return 500, 512, nil
	case "medium":
		return 1000, 1024, nil
	case "large":
		return 1500, 1536, nil
	default:
		return 0, 0, errors.New("instanceClass must be one of small, medium, large")
	}
}

func ParseExposure(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ExposurePublic:
		return ExposurePublic, nil
	case ExposurePrivate:
		return ExposurePrivate, nil
	default:
		return "", errors.New("exposure must be public or private")
	}
}

type WorkItem struct {
	Action        string
	ExecutionID   string
	PlanID        string
	NodeID        string
	ServiceID     string
	ServiceName   string
	Image         string
	Command       []string
	Args          []string
	Env           map[string]string
	ContainerPort int
	ReadinessPath string
	ContainerName string
	ContainerID   string
	HostPort      int
}

type ExecutionRecord struct {
	ID            string
	PlanID        string
	NodeID        string
	Image         string
	ContainerName string
	ContainerID   string
	ContainerPort int
	HostPort      int
	ReadinessPath string
	Status        string
	StatusReason  string
	StartedAt     time.Time
	FinishedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ReportInput struct {
	Status        string
	Reason        string
	ContainerID   string
	ContainerName string
	HostPort      int
}

type ReportAck struct {
	Execution  ExecutionRecord
	ObservedAt time.Time
}

func IsExecutionStatus(status string) bool {
	switch status {
	case StatusDeploying, StatusRunning, StatusSucceeded, StatusSuperseded, StatusFailed:
		return true
	default:
		return false
	}
}

func (in ReportInput) Validate() error {
	if !IsExecutionStatus(in.Status) {
		return errors.New("status must be one of deploying, running, succeeded, superseded, failed")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return errors.New("reason is required")
	}
	if strings.TrimSpace(in.ContainerName) == "" {
		return errors.New("containerName is required")
	}
	if in.Status == StatusRunning && in.HostPort <= 0 {
		return errors.New("hostPort must be greater than 0 when status is running")
	}
	return nil
}
