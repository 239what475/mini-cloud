package model

import "time"

const (
	StatusRegistering = "registering"
	StatusReady       = "ready"
	StatusDegraded    = "degraded"
	StatusOffline     = "offline"
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

type PlaneRegistration struct {
	Registered     bool       `json:"registered"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	TokenUpdatedAt *time.Time `json:"tokenUpdatedAt,omitempty"`
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

type PlaneNode struct {
	PlaneID           string     `json:"planeID"`
	NodeID            string     `json:"nodeID"`
	Name              string     `json:"name"`
	Provider          string     `json:"provider"`
	Region            string     `json:"region"`
	InstanceID        string     `json:"instanceID"`
	InstanceType      string     `json:"instanceType"`
	Status            string     `json:"status"`
	Schedulable       bool       `json:"schedulable"`
	Elastic           bool       `json:"elastic"`
	CPUMilliCapacity  int        `json:"cpuMilliCapacity"`
	CPUMilliAllocated int        `json:"cpuMilliAllocated"`
	MemoryMiCapacity  int        `json:"memoryMiCapacity"`
	MemoryMiAllocated int        `json:"memoryMiAllocated"`
	LastHeartbeatAt   *time.Time `json:"lastHeartbeatAt,omitempty"`
	ObservedAt        time.Time  `json:"observedAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type PlaneDetail struct {
	Plane
	Status                 PlaneStatus               `json:"status"`
	Registration           PlaneRegistration         `json:"registration"`
	LatestRuntimeInventory *RuntimeInventorySnapshot `json:"latestRuntimeInventory,omitempty"`
}

func IsStatus(status string) bool {
	switch status {
	case StatusRegistering, StatusReady, StatusDegraded, StatusOffline:
		return true
	default:
		return false
	}
}
