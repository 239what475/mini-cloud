package cloudplaneapi

import "time"

type SnapshotResponse struct {
	Plane         PlaneSummary          `json:"plane"`
	Health        HealthSummary         `json:"health"`
	Overview      OverviewSummary       `json:"overview"`
	Capacity      CapacitySummary       `json:"capacity"`
	Reliability   ReliabilitySummary    `json:"reliability"`
	Runtime       RuntimeInventory      `json:"runtimeInventory"`
	RuntimeConfig RuntimeConfigSnapshot `json:"runtimeConfig"`
	Executions    []ExecutionSnapshot   `json:"executions"`
}

type PlaneSummary struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	Configured bool   `json:"configured"`
}

type HealthSummary struct {
	CheckedAt time.Time `json:"checkedAt"`
	Service   string    `json:"service"`
	Database  string    `json:"database"`
}

type OverviewSummary struct {
	// Overview 描述 plane 的整体运行概况。
	ServicesTotal     int `json:"servicesTotal"`
	ServicesDeploying int `json:"servicesDeploying"`
	ServicesRunning   int `json:"servicesRunning"`
	ServicesDegraded  int `json:"servicesDegraded"`
	ServicesFailed    int `json:"servicesFailed"`

	NodesTotal       int `json:"nodesTotal"`
	NodesRegistering int `json:"nodesRegistering"`
	NodesReady       int `json:"nodesReady"`
	NodesNotReady    int `json:"nodesNotReady"`
	NodesDraining    int `json:"nodesDraining"`
	NodesOffline     int `json:"nodesOffline"`

	ExecutionPlansTotal     int `json:"executionPlansTotal"`
	ExecutionPlansPending   int `json:"executionPlansPending"`
	ExecutionPlansDeploying int `json:"executionPlansDeploying"`
	ExecutionPlansRunning   int `json:"executionPlansRunning"`
	ExecutionPlansFailed    int `json:"executionPlansFailed"`
}

type CapacitySummary struct {
	// Capacity 只描述 runtime 供给侧。
	RuntimeNodesTotal   int `json:"runtimeNodesTotal"`
	RuntimeNodesReady   int `json:"runtimeNodesReady"`
	CPUMilliTotal       int `json:"cpuMilliTotal"`
	CPUMilliAllocatable int `json:"cpuMilliAllocatable"`
	CPUMilliAllocated   int `json:"cpuMilliAllocated"`
	MemoryMiTotal       int `json:"memoryMiTotal"`
	MemoryMiAllocatable int `json:"memoryMiAllocatable"`
	MemoryMiAllocated   int `json:"memoryMiAllocated"`
}

type ReliabilitySummary struct {
	AlertsFiring int `json:"alertsFiring"`
}

type RuntimeInventory struct {
	SyncVersion int64             `json:"syncVersion"`
	ObservedAt  time.Time         `json:"observedAt"`
	Nodes       []RuntimeNodeView `json:"nodes"`
}

type RuntimeNodeView struct {
	NodeID              string     `json:"nodeID"`
	NodeEpoch           int64      `json:"nodeEpoch"`
	Name                string     `json:"name"`
	Provider            string     `json:"provider"`
	Region              string     `json:"region"`
	InstanceID          string     `json:"instanceID"`
	InstanceType        string     `json:"instanceType"`
	Status              string     `json:"status"`
	Schedulable         bool       `json:"schedulable"`
	CPUMilliTotal       int        `json:"cpuMilliTotal"`
	CPUMilliAllocatable int        `json:"cpuMilliAllocatable"`
	CPUMilliAllocated   int        `json:"cpuMilliAllocated"`
	MemoryMiTotal       int        `json:"memoryMiTotal"`
	MemoryMiAllocatable int        `json:"memoryMiAllocatable"`
	MemoryMiAllocated   int        `json:"memoryMiAllocated"`
	LastHeartbeatAt     *time.Time `json:"lastHeartbeatAt,omitempty"`
}

type RuntimeConfigSnapshot struct {
	ObservedAt  time.Time      `json:"observedAt"`
	Fingerprint string         `json:"fingerprint"`
	Summary     map[string]any `json:"summary"`
}

type ExecutionSnapshot struct {
	PlanID            string    `json:"planID"`
	ServiceID         string    `json:"serviceID"`
	ServiceName       string    `json:"serviceName"`
	ServiceGeneration int64     `json:"serviceGeneration"`
	Status            string    `json:"status"`
	LastStatusReason  string    `json:"lastStatusReason"`
	ObservedAt        time.Time `json:"observedAt"`
}

const (
	ApplyActionCreated = "created"
	ApplyActionUpdated = "updated"
)

type ExecutionProjectedFile struct {
	MountPath string `json:"mountPath"`
	Content   string `json:"content"`
	Mode      uint32 `json:"mode"`
	Sensitive bool   `json:"sensitive"`
}

type ExecutionImageCredential struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ExecutionPlanRequest struct {
	PlanID            string                    `json:"planID"`
	ServiceID         string                    `json:"serviceID"`
	ServiceName       string                    `json:"serviceName"`
	ServiceGeneration int64                     `json:"serviceGeneration"`
	Image             string                    `json:"image"`
	Command           []string                  `json:"command"`
	Args              []string                  `json:"args"`
	Env               map[string]string         `json:"env"`
	ProjectedFiles    []ExecutionProjectedFile  `json:"projectedFiles,omitempty"`
	ImageCredential   *ExecutionImageCredential `json:"imageCredential,omitempty"`
	ContainerPort     int                       `json:"containerPort"`
	ReadinessPath     string                    `json:"readinessPath"`
	InstanceClass     string                    `json:"instanceClass"`
	Exposure          string                    `json:"exposure"`
}

type ExecutionPlanResponse struct {
	Action string `json:"action"`
	PlanID string `json:"planID"`
}

type DeleteExecutionPlanRequest struct {
	ServiceID         string `json:"serviceID"`
	ServiceGeneration int64  `json:"serviceGeneration"`
	PlanID            string `json:"planID"`
}
