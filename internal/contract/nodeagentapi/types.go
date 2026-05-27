package nodeagentapi

import (
	"errors"
	"net"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

const (
	NodeStatusRegistering = "registering"
	NodeStatusReady       = "ready"
	NodeStatusNotReady    = "not_ready"
	NodeStatusDraining    = "draining"
	NodeStatusOffline     = "offline"

	ExecutionStatusDeploying  = "deploying"
	ExecutionStatusRunning    = "running"
	ExecutionStatusSuperseded = "superseded"
	ExecutionStatusFailed     = "failed"
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
	ErrNodeIDRequired             = errors.New("nodeID is required")
	ErrReportedAtRequired         = errors.New("reportedAt is required")
	ErrAgentVersionRequired       = errors.New("agentVersion is required")
	ErrInvalidCPUMilliAllocatable = errors.New("cpuMilliAllocatable must be greater than 0")
	ErrInvalidMemoryMiAllocatable = errors.New("memoryMiAllocatable must be greater than 0")
	ErrInvalidRunningContainers   = errors.New("runningContainers must be greater than or equal to 0")
	ErrInvalidNodeStatus          = errors.New("node status must be one of registering, ready, not_ready, draining, offline")
	ErrExecutionStatusInvalid     = errors.New("execution status must be one of deploying, running, superseded, failed")
	ErrReasonRequired             = errors.New("reason is required")
	ErrContainerNameRequired      = errors.New("containerName is required")
	ErrHostPortInvalid            = errors.New("hostPort must be greater than 0 when status is running")
	ErrSessionTokenRequired       = errors.New("sessionToken is required")
	ErrAcceptedAtRequired         = errors.New("acceptedAt is required")
	ErrReceivedAtRequired         = errors.New("receivedAt is required")
	ErrWorkExecutionIDRequired    = errors.New("executionID is required")
	ErrDeploymentIDRequired       = errors.New("deploymentID is required")
	ErrProjectIDRequired          = errors.New("projectID is required")
	ErrServiceIDRequired          = errors.New("serviceID is required")
	ErrImageRequired              = errors.New("image is required")
	ErrContainerPortInvalid       = errors.New("containerPort must be greater than 0")
	ErrReadinessPathRequired      = errors.New("readinessPath is required")
	ErrReadinessPathInvalid       = errors.New("readinessPath must start with /")
	ErrExecutionIDRequired        = errors.New("executionID is required")
	ErrObservedAtRequired         = errors.New("observedAt is required")
)

type RegisterNodeRequest struct {
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

type RegisterNodeResponse struct {
	NodeID         string    `json:"nodeID"`
	SessionToken   string    `json:"sessionToken"`
	ObservedStatus string    `json:"observedStatus"`
	AcceptedAt     time.Time `json:"acceptedAt"`
}

type HeartbeatRequest struct {
	ReportedAt          time.Time `json:"reportedAt"`
	AgentVersion        string    `json:"agentVersion"`
	CPUMilliAllocatable int       `json:"cpuMilliAllocatable"`
	MemoryMiAllocatable int       `json:"memoryMiAllocatable"`
	RunningContainers   int       `json:"runningContainers"`
	Status              string    `json:"status"`
}

type HeartbeatResponse struct {
	NodeID         string    `json:"nodeID"`
	Accepted       bool      `json:"accepted"`
	ObservedStatus string    `json:"observedStatus"`
	ReceivedAt     time.Time `json:"receivedAt"`
}

type WorkItem struct {
	ExecutionID         string                `json:"executionID"`
	DeploymentID        string                `json:"deploymentID"`
	ReplicaIndex        int                   `json:"replicaIndex"`
	NodeID              string                `json:"nodeID"`
	ProjectID           string                `json:"projectID"`
	ServiceID           string                `json:"serviceID"`
	ServiceName         string                `json:"serviceName"`
	RevisionID          string                `json:"revisionID"`
	RevisionLabel       string                `json:"revisionLabel"`
	Image               string                `json:"image"`
	Command             []string              `json:"command"`
	Args                []string              `json:"args"`
	Env                 map[string]string     `json:"env"`
	ProjectedFiles      []projectedfile.File  `json:"projectedFiles,omitempty"`
	PersistentDirs      []persistentdir.Mount `json:"persistentDirs,omitempty"`
	ImageCredential     *ImageCredential      `json:"imageCredential,omitempty"`
	SupersededExecution *SupersededExecution  `json:"supersededExecution,omitempty"`
	ContainerPort       int                   `json:"containerPort"`
	ReadinessPath       string                `json:"readinessPath"`
	ContainerName       string                `json:"containerName"`
}

type ImageCredential struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type SupersededExecution struct {
	DeploymentID  string `json:"deploymentID"`
	ExecutionID   string `json:"executionID"`
	ContainerID   string `json:"containerID"`
	ContainerName string `json:"containerName"`
}

type PollWorkResponse struct {
	Item *WorkItem `json:"item"`
}

type ReportExecutionRequest struct {
	Status                string `json:"status"`
	Reason                string `json:"reason"`
	ContainerID           string `json:"containerID"`
	ContainerName         string `json:"containerName"`
	HostPort              int    `json:"hostPort"`
	SupersededExecutionID string `json:"supersededExecutionID"`
}

type ExecutionRecord struct {
	ID            string     `json:"id"`
	DeploymentID  string     `json:"deploymentID"`
	NodeID        string     `json:"nodeID"`
	Image         string     `json:"image"`
	ContainerName string     `json:"containerName"`
	ContainerID   string     `json:"containerID"`
	ContainerPort int        `json:"containerPort"`
	HostPort      int        `json:"hostPort"`
	ReadinessPath string     `json:"readinessPath"`
	Status        string     `json:"status"`
	StatusReason  string     `json:"statusReason"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type ReportExecutionAck struct {
	Execution  ExecutionRecord `json:"execution"`
	ObservedAt time.Time       `json:"observedAt"`
}

type ReportExecutionResponse struct {
	Ack ReportExecutionAck `json:"ack"`
}

func (in RegisterNodeRequest) Validate() error {
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

func (in HeartbeatRequest) Validate() error {
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
		return ErrInvalidNodeStatus
	}
	return nil
}

func (in ReportExecutionRequest) Validate() error {
	if !IsExecutionStatus(in.Status) {
		return ErrExecutionStatusInvalid
	}
	if strings.TrimSpace(in.Reason) == "" {
		return ErrReasonRequired
	}
	if strings.TrimSpace(in.ContainerName) == "" {
		return ErrContainerNameRequired
	}
	if in.Status == ExecutionStatusRunning && in.HostPort <= 0 {
		return ErrHostPortInvalid
	}
	return nil
}

func (in RegisterNodeResponse) Validate() error {
	if strings.TrimSpace(in.NodeID) == "" {
		return ErrNodeIDRequired
	}
	if strings.TrimSpace(in.SessionToken) == "" {
		return ErrSessionTokenRequired
	}
	if !IsNodeStatus(in.ObservedStatus) {
		return ErrInvalidNodeStatus
	}
	if in.AcceptedAt.IsZero() {
		return ErrAcceptedAtRequired
	}
	return nil
}

func (in HeartbeatResponse) Validate() error {
	if strings.TrimSpace(in.NodeID) == "" {
		return ErrNodeIDRequired
	}
	if !IsNodeStatus(in.ObservedStatus) {
		return ErrInvalidNodeStatus
	}
	if in.ReceivedAt.IsZero() {
		return ErrReceivedAtRequired
	}
	return nil
}

func (in WorkItem) Validate() error {
	if strings.TrimSpace(in.ExecutionID) == "" {
		return ErrWorkExecutionIDRequired
	}
	if strings.TrimSpace(in.DeploymentID) == "" {
		return ErrDeploymentIDRequired
	}
	if strings.TrimSpace(in.NodeID) == "" {
		return ErrNodeIDRequired
	}
	if strings.TrimSpace(in.ProjectID) == "" {
		return ErrProjectIDRequired
	}
	if strings.TrimSpace(in.ServiceID) == "" {
		return ErrServiceIDRequired
	}
	if strings.TrimSpace(in.Image) == "" {
		return ErrImageRequired
	}
	if in.ContainerPort <= 0 {
		return ErrContainerPortInvalid
	}
	readinessPath := strings.TrimSpace(in.ReadinessPath)
	if readinessPath == "" {
		return ErrReadinessPathRequired
	}
	if !strings.HasPrefix(readinessPath, "/") {
		return ErrReadinessPathInvalid
	}
	if strings.TrimSpace(in.ContainerName) == "" {
		return ErrContainerNameRequired
	}
	for _, item := range in.ProjectedFiles {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	for _, item := range in.PersistentDirs {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (in ReportExecutionResponse) Validate() error {
	if strings.TrimSpace(in.Ack.Execution.ID) == "" {
		return ErrExecutionIDRequired
	}
	if !IsExecutionStatus(in.Ack.Execution.Status) {
		return ErrExecutionStatusInvalid
	}
	if in.Ack.ObservedAt.IsZero() {
		return ErrObservedAtRequired
	}
	return nil
}

func IsNodeStatus(status string) bool {
	switch status {
	case NodeStatusRegistering, NodeStatusReady, NodeStatusNotReady, NodeStatusDraining, NodeStatusOffline:
		return true
	default:
		return false
	}
}

func IsExecutionStatus(status string) bool {
	switch status {
	case ExecutionStatusDeploying, ExecutionStatusRunning, ExecutionStatusSuperseded, ExecutionStatusFailed:
		return true
	default:
		return false
	}
}
