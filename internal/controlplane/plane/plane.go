package plane

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	StatusRegistering = "registering"
	StatusReady       = "ready"
	StatusDegraded    = "degraded"
	StatusOffline     = "offline"

	OperationStateActive      = "active"
	OperationStateMaintenance = "maintenance"
	OperationStateDraining    = "draining"
)

var (
	ErrPlaneNameRequired             = errors.New("name is required")
	ErrInvalidPlaneName              = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrPlaneDisplayNameRequired      = errors.New("displayName is required")
	ErrPlaneProviderRequired         = errors.New("provider is required")
	ErrPlaneRegionRequired           = errors.New("region is required")
	ErrPlaneGRPCEndpointRequired     = errors.New("grpcEndpoint is required")
	ErrPlaneSouthboundTokenRequired  = errors.New("southboundToken is required")
	ErrInvalidPlaneGRPCEndpoint      = errors.New("grpcEndpoint must be a gRPC target such as host:port, grpc://host:port, grpcs://host:port, dns:///name:port, or unix:///path")
	ErrInvalidPlaneStatus            = errors.New("status must be one of registering, ready, degraded, offline")
	ErrInvalidPlaneOperationState    = errors.New("operation state must be one of active, maintenance, draining")
	ErrPlaneOperationReasonRequired  = errors.New("reason is required when operation state is maintenance or draining")
	ErrInvalidNodesTotal             = errors.New("nodesTotal must be greater than or equal to 0")
	ErrInvalidNodesReady             = errors.New("nodesReady must be greater than or equal to 0")
	ErrInvalidNodesReadyExceedsTotal = errors.New("nodesReady must be less than or equal to nodesTotal")
	ErrInvalidServicesTotal          = errors.New("servicesTotal must be greater than or equal to 0")
	ErrInvalidDeploymentsTotal       = errors.New("deploymentsTotal must be greater than or equal to 0")
	ErrInvalidCPUMilliCapacity       = errors.New("cpuMilliCapacity must be greater than or equal to 0")
	ErrInvalidCPUMilliAllocated      = errors.New("cpuMilliAllocated must be greater than or equal to 0")
	ErrInvalidCPUMilliAllocation     = errors.New("cpuMilliAllocated must be less than or equal to cpuMilliCapacity")
	ErrInvalidMemoryMiCapacity       = errors.New("memoryMiCapacity must be greater than or equal to 0")
	ErrInvalidMemoryMiAllocated      = errors.New("memoryMiAllocated must be greater than or equal to 0")
	ErrInvalidMemoryMiAllocation     = errors.New("memoryMiAllocated must be less than or equal to memoryMiCapacity")
	planeNamePattern                 = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Plane struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"displayName"`
	Provider     string    `json:"provider"`
	Region       string    `json:"region"`
	GRPCEndpoint string    `json:"grpcEndpoint"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PlaneStatus struct {
	PlaneID              string     `json:"planeID"`
	Status               string     `json:"status"`
	Message              string     `json:"message"`
	LastHeartbeatAt      *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt           *time.Time `json:"lastSyncAt,omitempty"`
	LastInventoryVersion int64      `json:"lastInventoryVersion"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type Registration struct {
	Registered     bool       `json:"registered"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	TokenUpdatedAt *time.Time `json:"tokenUpdatedAt,omitempty"`
}

type Operation struct {
	PlaneID   string    `json:"planeID"`
	State     string    `json:"state"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CapacitySnapshot struct {
	// 这里的 Nodes* 表示 control-plane 看到的“plane 供给侧节点计数”。
	// 当前实现中，它同步自远端 plane 的 Capacity.RuntimeNodes*，
	// 也就是 role=runtime 的 node 汇总，而不是更泛化的 Overview.Nodes*。
	ID                string    `json:"id"`
	PlaneID           string    `json:"planeID"`
	NodesTotal        int       `json:"nodesTotal"`
	NodesReady        int       `json:"nodesReady"`
	ServicesTotal     int       `json:"servicesTotal"`
	DeploymentsTotal  int       `json:"deploymentsTotal"`
	CPUMilliCapacity  int       `json:"cpuMilliCapacity"`
	CPUMilliAllocated int       `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int       `json:"memoryMiCapacity"`
	MemoryMiAllocated int       `json:"memoryMiAllocated"`
	CapturedAt        time.Time `json:"capturedAt"`
}

type RuntimeInventorySnapshot struct {
	PlaneID           string    `json:"planeID"`
	SyncVersion       int64     `json:"syncVersion"`
	ObservedAt        time.Time `json:"observedAt"`
	NodesTotal        int       `json:"nodesTotal"`
	NodesReady        int       `json:"nodesReady"`
	CPUMilliCapacity  int       `json:"cpuMilliCapacity"`
	CPUMilliAllocated int       `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int       `json:"memoryMiCapacity"`
	MemoryMiAllocated int       `json:"memoryMiAllocated"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type RuntimeConfigSnapshot struct {
	PlaneID     string         `json:"planeID"`
	ObservedAt  time.Time      `json:"observedAt"`
	Fingerprint string         `json:"fingerprint"`
	Summary     map[string]any `json:"summary"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type RuntimeNode struct {
	PlaneID           string     `json:"planeID"`
	NodeID            string     `json:"nodeID"`
	NodeEpoch         int64      `json:"nodeEpoch"`
	Name              string     `json:"name"`
	Provider          string     `json:"provider"`
	Region            string     `json:"region"`
	InstanceID        string     `json:"instanceID"`
	InstanceType      string     `json:"instanceType"`
	Status            string     `json:"status"`
	Schedulable       bool       `json:"schedulable"`
	CPUMilliCapacity  int        `json:"cpuMilliCapacity"`
	CPUMilliAllocated int        `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int        `json:"memoryMiCapacity"`
	MemoryMiAllocated int        `json:"memoryMiAllocated"`
	LastHeartbeatAt   *time.Time `json:"lastHeartbeatAt,omitempty"`
	ObservedAt        time.Time  `json:"observedAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Detail struct {
	Plane
	Status                 PlaneStatus               `json:"status"`
	Registration           Registration              `json:"registration"`
	Operation              Operation                 `json:"operation"`
	LatestCapacityRecord   *CapacitySnapshot         `json:"latestCapacitySnapshot,omitempty"`
	LatestRuntimeInventory *RuntimeInventorySnapshot `json:"latestRuntimeInventory,omitempty"`
	LatestRuntimeConfig    *RuntimeConfigSnapshot    `json:"latestRuntimeConfig,omitempty"`
}

type CreateInput struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	GRPCEndpoint string `json:"grpcEndpoint"`
}

type UpdateStatusInput struct {
	Status          string     `json:"status"`
	Message         string     `json:"message"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
}

type RegisterInput struct {
	SouthboundToken string `json:"southboundToken"`
}

type UpdateOperationInput struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type RecordCapacitySnapshotInput struct {
	// 这里沿用 Nodes* 命名，是因为 control-plane 里的 node 仍然就是“节点”这一层实体；
	// 真正需要单独区分的是 runtime node 生命周期记录，它在 cloud-plane 里由 runtimenode.Record 表达。
	NodesTotal        int       `json:"nodesTotal"`
	NodesReady        int       `json:"nodesReady"`
	ServicesTotal     int       `json:"servicesTotal"`
	DeploymentsTotal  int       `json:"deploymentsTotal"`
	CPUMilliCapacity  int       `json:"cpuMilliCapacity"`
	CPUMilliAllocated int       `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int       `json:"memoryMiCapacity"`
	MemoryMiAllocated int       `json:"memoryMiAllocated"`
	CapturedAt        time.Time `json:"capturedAt,omitempty"`
}

type RecordRuntimeInventoryInput struct {
	SyncVersion       int64         `json:"syncVersion"`
	ObservedAt        time.Time     `json:"observedAt"`
	NodesTotal        int           `json:"nodesTotal"`
	NodesReady        int           `json:"nodesReady"`
	CPUMilliCapacity  int           `json:"cpuMilliCapacity"`
	CPUMilliAllocated int           `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int           `json:"memoryMiCapacity"`
	MemoryMiAllocated int           `json:"memoryMiAllocated"`
	Nodes             []RuntimeNode `json:"nodes"`
}

type RecordRuntimeConfigInput struct {
	ObservedAt  time.Time      `json:"observedAt"`
	Fingerprint string         `json:"fingerprint"`
	Summary     map[string]any `json:"summary"`
}

func (in CreateInput) Validate() error {
	switch {
	case strings.TrimSpace(in.Name) == "":
		return ErrPlaneNameRequired
	case !planeNamePattern.MatchString(strings.TrimSpace(in.Name)):
		return ErrInvalidPlaneName
	case strings.TrimSpace(in.DisplayName) == "":
		return ErrPlaneDisplayNameRequired
	case strings.TrimSpace(in.Provider) == "":
		return ErrPlaneProviderRequired
	case strings.TrimSpace(in.Region) == "":
		return ErrPlaneRegionRequired
	}

	_, err := normalizeGRPCEndpoint(in.GRPCEndpoint)
	return err
}

func (in CreateInput) ResolvedGRPCEndpoint() (string, error) {
	return normalizeGRPCEndpoint(in.GRPCEndpoint)
}

func (in UpdateStatusInput) Validate() error {
	if !IsStatus(in.Status) {
		return ErrInvalidPlaneStatus
	}
	return nil
}

func (in RegisterInput) Validate() error {
	if strings.TrimSpace(in.SouthboundToken) == "" {
		return ErrPlaneSouthboundTokenRequired
	}
	return nil
}

func (in UpdateOperationInput) Validate() error {
	if !IsOperationState(in.State) {
		return ErrInvalidPlaneOperationState
	}
	if in.State != OperationStateActive && strings.TrimSpace(in.Reason) == "" {
		return ErrPlaneOperationReasonRequired
	}
	return nil
}

func (in UpdateOperationInput) ResolvedReason() string {
	if in.State == OperationStateActive {
		return ""
	}
	return strings.TrimSpace(in.Reason)
}

func (in RecordCapacitySnapshotInput) Validate() error {
	switch {
	case in.NodesTotal < 0:
		return ErrInvalidNodesTotal
	case in.NodesReady < 0:
		return ErrInvalidNodesReady
	case in.NodesReady > in.NodesTotal:
		return ErrInvalidNodesReadyExceedsTotal
	case in.ServicesTotal < 0:
		return ErrInvalidServicesTotal
	case in.DeploymentsTotal < 0:
		return ErrInvalidDeploymentsTotal
	case in.CPUMilliCapacity < 0:
		return ErrInvalidCPUMilliCapacity
	case in.CPUMilliAllocated < 0:
		return ErrInvalidCPUMilliAllocated
	case in.CPUMilliAllocated > in.CPUMilliCapacity:
		return ErrInvalidCPUMilliAllocation
	case in.MemoryMiCapacity < 0:
		return ErrInvalidMemoryMiCapacity
	case in.MemoryMiAllocated < 0:
		return ErrInvalidMemoryMiAllocated
	case in.MemoryMiAllocated > in.MemoryMiCapacity:
		return ErrInvalidMemoryMiAllocation
	default:
		return nil
	}
}

func (in RecordCapacitySnapshotInput) ResolvedCapturedAt(now time.Time) time.Time {
	if in.CapturedAt.IsZero() {
		return now.UTC()
	}
	return in.CapturedAt.UTC()
}

func (in RecordRuntimeInventoryInput) ResolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func (in RecordRuntimeConfigInput) ResolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func IsStatus(status string) bool {
	switch status {
	case StatusRegistering, StatusReady, StatusDegraded, StatusOffline:
		return true
	default:
		return false
	}
}

func IsOperationState(state string) bool {
	switch state {
	case OperationStateActive, OperationStateMaintenance, OperationStateDraining:
		return true
	default:
		return false
	}
}

func OperationStateAcceptingNewDeployments(state string) bool {
	return state == OperationStateActive
}

func (o Operation) ResolvedState() string {
	if IsOperationState(o.State) {
		return o.State
	}
	return OperationStateActive
}

func (o Operation) AcceptingNewDeployments() bool {
	return OperationStateAcceptingNewDeployments(o.ResolvedState())
}

func normalizeGRPCEndpoint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrPlaneGRPCEndpointRequired
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", ErrInvalidPlaneGRPCEndpoint
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", ErrInvalidPlaneGRPCEndpoint
	}
	if strings.HasPrefix(lower, "grpc://") {
		target := strings.TrimSpace(value[len("grpc://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", ErrInvalidPlaneGRPCEndpoint
		}
		return target, nil
	}
	if strings.HasPrefix(lower, "grpcs://") || strings.HasPrefix(lower, "dns:///") || strings.HasPrefix(lower, "unix:///") {
		return value, nil
	}
	return value, nil
}
