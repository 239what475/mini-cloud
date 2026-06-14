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
}

type PlaneStatus struct {
	PlaneID         string
	Status          string
	Message         string
	LastHeartbeatAt *time.Time
	LastSyncAt      *time.Time
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
}

type PlaneDetail struct {
	Plane
	Status PlaneStatus
}

func IsPlaneStatus(status string) bool {
	switch status {
	case StatusSyncing, StatusReady, StatusDegraded, StatusOffline:
		return true
	default:
		return false
	}
}
