package model

import "time"

const (
	StatusSyncing  = "syncing"
	StatusReady    = "ready"
	StatusDegraded = "degraded"
	StatusOffline  = "offline"
)

type Plane struct {
	ID           string
	Name         string
	DisplayName  string
	Provider     string
	Region       string
	GRPCEndpoint string
	CreatedAt    time.Time
}

type PlaneStatus struct {
	PlaneID         string
	Status          string
	Message         string
	LastHeartbeatAt *time.Time
	LastSyncAt      *time.Time
	UpdatedAt       time.Time
}

type NodeInventorySnapshot struct {
	PlaneID           string
	ObservedAt        time.Time
	NodesTotal        int
	NodesReady        int
	CPUMilliCapacity  int
	CPUMilliAllocated int
	MemoryMiCapacity  int
	MemoryMiAllocated int
	UpdatedAt         time.Time
}

type PlaneNode struct {
	PlaneID           string
	NodeID            string
	Name              string
	Provider          string
	Region            string
	InstanceID        string
	InstanceType      string
	Status            string
	Schedulable       bool
	Elastic           bool
	CPUMilliCapacity  int
	CPUMilliAllocated int
	MemoryMiCapacity  int
	MemoryMiAllocated int
	LastHeartbeatAt   *time.Time
	ObservedAt        time.Time
	UpdatedAt         time.Time
}

type PlaneDetail struct {
	Plane
	Status              PlaneStatus
	LatestNodeInventory *NodeInventorySnapshot
}

func IsPlaneStatus(status string) bool {
	switch status {
	case StatusSyncing, StatusReady, StatusDegraded, StatusOffline:
		return true
	default:
		return false
	}
}
