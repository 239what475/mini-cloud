package model

import (
	"errors"
	"net"
	"strings"
	"time"
)

const (
	StatusProvisioning = "provisioning"
	StatusRegistering  = "registering"
	StatusReady        = "ready"
	StatusNotReady     = "not_ready"
	StatusDraining     = "draining"
	StatusOffline      = "offline"
	StatusDeleted      = "deleted"
)

var (
	ErrProviderRequired           = errors.New("provider is required")
	ErrRegionRequired             = errors.New("region is required")
	ErrNodeNameRequired           = errors.New("name is required")
	ErrPrivateIPRequired          = errors.New("privateIP is required")
	ErrInvalidPrivateIP           = errors.New("privateIP must be a valid IP address")
	ErrInvalidPublicIP            = errors.New("publicIP must be a valid IP address")
	ErrInstanceIDRequired         = errors.New("instanceID is required")
	ErrInstanceTypeRequired       = errors.New("instanceType is required")
	ErrInvalidCPUMilliTotal       = errors.New("cpuMilliTotal must be greater than 0")
	ErrInvalidMemoryMiTotal       = errors.New("memoryMiTotal must be greater than 0")
	ErrReportedAtRequired         = errors.New("reportedAt is required")
	ErrAgentVersionRequired       = errors.New("agentVersion is required")
	ErrInvalidCPUMilliAllocatable = errors.New("cpuMilliAllocatable must be greater than 0")
	ErrInvalidMemoryMiAllocatable = errors.New("memoryMiAllocatable must be greater than 0")
	ErrInvalidRunningContainers   = errors.New("runningContainers must be greater than or equal to 0")
	ErrInvalidStatus              = errors.New("status must be one of provisioning, registering, ready, not_ready, draining, offline, deleted")
)

type Node struct {
	ID                  string     `json:"id"`
	Provider            string     `json:"provider"`
	Region              string     `json:"region"`
	Name                string     `json:"name"`
	PrivateIP           string     `json:"privateIP"`
	PublicIP            string     `json:"publicIP"`
	InstanceID          string     `json:"instanceID"`
	InstanceType        string     `json:"instanceType"`
	CPUMilliTotal       int        `json:"cpuMilliTotal"`
	MemoryMiTotal       int        `json:"memoryMiTotal"`
	CPUMilliAllocatable int        `json:"cpuMilliAllocatable"`
	MemoryMiAllocatable int        `json:"memoryMiAllocatable"`
	CPUMilliAllocated   int        `json:"cpuMilliAllocated"`
	MemoryMiAllocated   int        `json:"memoryMiAllocated"`
	Status              string     `json:"status"`
	StatusReason        string     `json:"statusReason"`
	Schedulable         bool       `json:"schedulable"`
	Elastic             bool       `json:"elastic"`
	LastHeartbeatAt     *time.Time `json:"lastHeartbeatAt"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type ProvisioningInput struct {
	Provider     string
	Region       string
	Name         string
	InstanceType string
	StatusReason string
}

func (in ProvisioningInput) Validate() error {
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	if strings.TrimSpace(in.Region) == "" {
		return ErrRegionRequired
	}
	if strings.TrimSpace(in.Name) == "" {
		return ErrNodeNameRequired
	}
	if strings.TrimSpace(in.InstanceType) == "" {
		return ErrInstanceTypeRequired
	}
	return nil
}

type HeartbeatSummary struct {
	ReportedAt          time.Time `json:"reportedAt"`
	AgentVersion        string    `json:"agentVersion"`
	CPUMilliAllocatable int       `json:"cpuMilliAllocatable"`
	MemoryMiAllocatable int       `json:"memoryMiAllocatable"`
	RunningContainers   int       `json:"runningContainers"`
	Status              string    `json:"status"`
}

type ReconcileImpact struct {
	NodeID      string `json:"nodeID"`
	NodeName    string `json:"nodeName"`
	PlanID      string `json:"planID"`
	ServiceID   string `json:"serviceID"`
	ServiceName string `json:"serviceName"`
	Reason      string `json:"reason"`
}

type HeartbeatReconcileResult struct {
	StaleAfterSeconds  int               `json:"staleAfterSeconds"`
	CutoffTime         time.Time         `json:"cutoffTime"`
	NodesMarkedOffline []Node            `json:"nodesMarkedOffline"`
	ImpactedPlans      []ReconcileImpact `json:"impactedPlans"`
}

type RegisterInput struct {
	Provider      string `json:"provider"`
	Region        string `json:"region"`
	Name          string `json:"name"`
	PrivateIP     string `json:"privateIP"`
	PublicIP      string `json:"publicIP"`
	InstanceID    string `json:"instanceID"`
	InstanceType  string `json:"instanceType"`
	CPUMilliTotal int    `json:"cpuMilliTotal"`
	MemoryMiTotal int    `json:"memoryMiTotal"`
}

func (in RegisterInput) Validate() error {
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	if strings.TrimSpace(in.Region) == "" {
		return ErrRegionRequired
	}
	if strings.TrimSpace(in.Name) == "" {
		return ErrNodeNameRequired
	}
	if strings.TrimSpace(in.PrivateIP) == "" {
		return ErrPrivateIPRequired
	}
	if net.ParseIP(strings.TrimSpace(in.PrivateIP)) == nil {
		return ErrInvalidPrivateIP
	}
	if publicIP := strings.TrimSpace(in.PublicIP); publicIP != "" && net.ParseIP(publicIP) == nil {
		return ErrInvalidPublicIP
	}
	if strings.TrimSpace(in.InstanceID) == "" {
		return ErrInstanceIDRequired
	}
	if strings.TrimSpace(in.InstanceType) == "" {
		return ErrInstanceTypeRequired
	}
	if in.CPUMilliTotal <= 0 {
		return ErrInvalidCPUMilliTotal
	}
	if in.MemoryMiTotal <= 0 {
		return ErrInvalidMemoryMiTotal
	}
	return nil
}

type HeartbeatInput struct {
	ReportedAt          time.Time `json:"reportedAt"`
	AgentVersion        string    `json:"agentVersion"`
	CPUMilliAllocatable int       `json:"cpuMilliAllocatable"`
	MemoryMiAllocatable int       `json:"memoryMiAllocatable"`
	RunningContainers   int       `json:"runningContainers"`
	Status              string    `json:"status"`
}

func (in HeartbeatInput) Validate() error {
	if in.ReportedAt.IsZero() {
		return ErrReportedAtRequired
	}
	if strings.TrimSpace(in.AgentVersion) == "" {
		return ErrAgentVersionRequired
	}
	if in.CPUMilliAllocatable <= 0 {
		return ErrInvalidCPUMilliAllocatable
	}
	if in.MemoryMiAllocatable <= 0 {
		return ErrInvalidMemoryMiAllocatable
	}
	if in.RunningContainers < 0 {
		return ErrInvalidRunningContainers
	}
	if !IsNodeStatus(in.Status) {
		return ErrInvalidStatus
	}
	return nil
}

func IsNodeStatus(status string) bool {
	switch status {
	case StatusProvisioning, StatusRegistering, StatusReady, StatusNotReady, StatusDraining, StatusOffline, StatusDeleted:
		return true
	default:
		return false
	}
}
