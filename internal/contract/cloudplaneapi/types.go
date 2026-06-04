package cloudplaneapi

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

var (
	// ErrConfigSetIDRequired 表示 config set 缺少 control-plane 分配的 ID。
	ErrConfigSetIDRequired = errors.New("configSetID is required")
	// ErrConfigSetNameRequired 表示 config set 缺少名称。
	ErrConfigSetNameRequired = errors.New("config set name is required")
	// ErrConfigSetNameInvalid 表示 config set 名称不符合平台命名规则。
	ErrConfigSetNameInvalid = errors.New("config set name must use lowercase letters, digits, and hyphens")
	// ErrConfigSetValuesRequired 表示 config set 至少需要一个键值。
	ErrConfigSetValuesRequired = errors.New("config set values must contain at least one item")
	// ErrConfigSetValueKeyInvalid 表示 config set 中存在空 key。
	ErrConfigSetValueKeyInvalid = errors.New("config keys must not be empty")

	// ErrSecretSetIDRequired 表示 secret set 缺少 control-plane 分配的 ID。
	ErrSecretSetIDRequired = errors.New("secretSetID is required")
	// ErrSecretSetNameRequired 表示 secret set 缺少名称。
	ErrSecretSetNameRequired = errors.New("secret set name is required")
	// ErrSecretSetNameInvalid 表示 secret set 名称不符合平台命名规则。
	ErrSecretSetNameInvalid = errors.New("secret set name must use lowercase letters, digits, and hyphens")
	// ErrSecretSetValuesRequired 表示 secret set 至少需要一个敏感键值。
	ErrSecretSetValuesRequired = errors.New("secret set values must contain at least one item")
	// ErrSecretSetValueKeyInvalid 表示 secret set 中存在空 key。
	ErrSecretSetValueKeyInvalid = errors.New("secret keys must not be empty")

	// ErrRegistryCredentialIDRequired 表示 registry credential 缺少 control-plane 分配的 ID。
	ErrRegistryCredentialIDRequired = errors.New("registryCredentialID is required")
	// ErrRegistryCredentialNameRequired 表示 registry credential 缺少名称。
	ErrRegistryCredentialNameRequired = errors.New("registry credential name is required")
	// ErrRegistryCredentialNameInvalid 表示 registry credential 名称不符合平台命名规则。
	ErrRegistryCredentialNameInvalid = errors.New("registry credential name must use lowercase letters, digits, and hyphens")
	// ErrRegistryCredentialServerRequired 表示 registry credential 缺少仓库地址。
	ErrRegistryCredentialServerRequired = errors.New("registry server is required")
	// ErrRegistryCredentialUsernameRequired 表示 registry credential 缺少用户名。
	ErrRegistryCredentialUsernameRequired = errors.New("registry username is required")
	// ErrRegistryCredentialPasswordRequired 表示 registry credential 缺少密码。
	ErrRegistryCredentialPasswordRequired = errors.New("registry password is required")

	resourceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type QuotaRejectReason struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Current        int32  `json:"current"`
	RequestedDelta int32  `json:"requestedDelta"`
	Projected      int32  `json:"projected"`
	Limit          int32  `json:"limit"`
}

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
	// 这里的 Nodes* 是“所有已知 node”的计数，不区分是否承担 runtime 供给。
	ServicesTotal     int `json:"servicesTotal"`
	ServicesIdle      int `json:"servicesIdle"`
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

	DeploymentsTotal      int `json:"deploymentsTotal"`
	DeploymentsPending    int `json:"deploymentsPending"`
	DeploymentsScheduling int `json:"deploymentsScheduling"`
	DeploymentsAssigned   int `json:"deploymentsAssigned"`
	DeploymentsDeploying  int `json:"deploymentsDeploying"`
	DeploymentsRunning    int `json:"deploymentsRunning"`
	DeploymentsFailed     int `json:"deploymentsFailed"`
}

type CapacitySummary struct {
	// Capacity 只描述 runtime 供给侧。
	// RuntimeNodes* 以及 CPU/内存容量都只统计 role=runtime 的 node，
	// platform node 不进入这份容量视图。
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
	PlanID             string    `json:"planID"`
	ServiceID          string    `json:"serviceID"`
	ServiceName        string    `json:"serviceName"`
	ServiceGeneration  int64     `json:"serviceGeneration"`
	DesiredReplicas    int       `json:"desiredReplicas"`
	DeployingReplicas  int       `json:"deployingReplicas"`
	RunningReplicas    int       `json:"runningReplicas"`
	FailedReplicas     int       `json:"failedReplicas"`
	SupersededReplicas int       `json:"supersededReplicas"`
	LastStatusReason   string    `json:"lastStatusReason"`
	ObservedAt         time.Time `json:"observedAt"`
}

type ResourceBundle struct {
	ConfigSets          []ConfigSet          `json:"configSets"`
	SecretSets          []SecretSet          `json:"secretSets"`
	RegistryCredentials []RegistryCredential `json:"registryCredentials"`
}

type ConfigSet struct {
	ID     string            `json:"configSetID"`
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

type SecretSet struct {
	ID     string            `json:"secretSetID"`
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

type RegistryCredential struct {
	ID       string `json:"registryCredentialID"`
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Validate 校验 config set 期望状态是否满足 cloud-plane 资源约束。
func (s ConfigSet) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return ErrConfigSetIDRequired
	}
	if strings.TrimSpace(s.Name) == "" {
		return ErrConfigSetNameRequired
	}
	if !resourceNamePattern.MatchString(strings.TrimSpace(s.Name)) {
		return ErrConfigSetNameInvalid
	}
	if len(s.Values) == 0 {
		return ErrConfigSetValuesRequired
	}
	for key := range s.Values {
		if strings.TrimSpace(key) == "" {
			return ErrConfigSetValueKeyInvalid
		}
	}
	return nil
}

// Validate 校验 secret set 期望状态是否满足 cloud-plane 资源约束。
func (s SecretSet) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return ErrSecretSetIDRequired
	}
	if strings.TrimSpace(s.Name) == "" {
		return ErrSecretSetNameRequired
	}
	if !resourceNamePattern.MatchString(strings.TrimSpace(s.Name)) {
		return ErrSecretSetNameInvalid
	}
	if len(s.Values) == 0 {
		return ErrSecretSetValuesRequired
	}
	for key := range s.Values {
		if strings.TrimSpace(key) == "" {
			return ErrSecretSetValueKeyInvalid
		}
	}
	return nil
}

// Validate 校验 registry credential 期望状态是否满足 cloud-plane 资源约束。
func (c RegistryCredential) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return ErrRegistryCredentialIDRequired
	}
	if strings.TrimSpace(c.Name) == "" {
		return ErrRegistryCredentialNameRequired
	}
	if !resourceNamePattern.MatchString(strings.TrimSpace(c.Name)) {
		return ErrRegistryCredentialNameInvalid
	}
	if strings.TrimSpace(c.Server) == "" {
		return ErrRegistryCredentialServerRequired
	}
	if strings.TrimSpace(c.Username) == "" {
		return ErrRegistryCredentialUsernameRequired
	}
	if strings.TrimSpace(c.Password) == "" {
		return ErrRegistryCredentialPasswordRequired
	}
	return nil
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

type ExecutionPersistentDir struct {
	Name       string `json:"name"`
	MountPath  string `json:"mountPath"`
	SourcePath string `json:"sourcePath"`
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
	PersistentDirs    []ExecutionPersistentDir  `json:"persistentDirs,omitempty"`
	ImageCredential   *ExecutionImageCredential `json:"imageCredential,omitempty"`
	ContainerPort     int                       `json:"containerPort"`
	ReadinessPath     string                    `json:"readinessPath"`
	Replicas          int                       `json:"replicas"`
	InstanceClass     string                    `json:"instanceClass"`
}

type ExecutionPlanResponse struct {
	Action string `json:"action"`
	PlanID string `json:"planID"`
}
