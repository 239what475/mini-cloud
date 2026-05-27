package runtimepool

import (
	"errors"
	"fmt"
	"strings"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
)

const (
	StatusPhaseHealthy       = "healthy"
	StatusPhaseSnapshotMiss  = "snapshot_missing"
	StatusPhaseBelowMinReady = "below_min_ready"
	StatusPhaseAboveMaxReady = "above_max_ready"
	StatusPhaseBelowHeadroom = "below_headroom"
)

var (
	ErrPlaneIDRequired        = errors.New("planeID is required")
	ErrInvalidMinReady        = errors.New("minReady must be greater than or equal to 0")
	ErrInvalidMaxReady        = errors.New("maxReady must be greater than or equal to 0")
	ErrInvalidMaxReadyLessMin = errors.New("maxReady must be greater than or equal to minReady")
	ErrInvalidHeadroomCPU     = errors.New("headroomCPUMilli must be greater than or equal to 0")
	ErrInvalidHeadroomMemory  = errors.New("headroomMemoryMi must be greater than or equal to 0")
)

type Pool struct {
	PlaneID          string    `json:"planeID"`
	MinReady         int       `json:"minReady"`
	MaxReady         int       `json:"maxReady"`
	HeadroomCPUMilli int       `json:"headroomCPUMilli"`
	HeadroomMemoryMi int       `json:"headroomMemoryMi"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type UpsertInput struct {
	MinReady         int `json:"minReady"`
	MaxReady         int `json:"maxReady"`
	HeadroomCPUMilli int `json:"headroomCPUMilli"`
	HeadroomMemoryMi int `json:"headroomMemoryMi"`
}

type SnapshotView struct {
	CapturedAt   *time.Time `json:"capturedAt,omitempty"`
	NodesTotal   int        `json:"nodesTotal"`
	NodesReady   int        `json:"nodesReady"`
	CPUMilliFree int        `json:"cpuMilliFree"`
	MemoryMiFree int        `json:"memoryMiFree"`
}

type StatusView struct {
	Phase           string `json:"phase"`
	Reason          string `json:"reason"`
	SnapshotMissing bool   `json:"snapshotMissing"`
	BelowMinReady   bool   `json:"belowMinReady"`
	AboveMaxReady   bool   `json:"aboveMaxReady"`
	BelowHeadroom   bool   `json:"belowHeadroom"`
}

type View struct {
	PlaneID          string       `json:"planeID"`
	PlaneName        string       `json:"planeName"`
	PlaneDisplayName string       `json:"planeDisplayName"`
	Provider         string       `json:"provider"`
	Region           string       `json:"region"`
	MinReady         int          `json:"minReady"`
	MaxReady         int          `json:"maxReady"`
	HeadroomCPUMilli int          `json:"headroomCPUMilli"`
	HeadroomMemoryMi int          `json:"headroomMemoryMi"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
	Status           StatusView   `json:"status"`
	Snapshot         SnapshotView `json:"snapshot"`
}

type Admission struct {
	Allowed           bool   `json:"allowed"`
	Reason            string `json:"reason"`
	CPUMilliFreeAfter int    `json:"cpuMilliFreeAfter"`
	MemoryMiFreeAfter int    `json:"memoryMiFreeAfter"`
}

func (in UpsertInput) Validate() error {
	switch {
	case in.MinReady < 0:
		return ErrInvalidMinReady
	case in.MaxReady < 0:
		return ErrInvalidMaxReady
	case in.MaxReady < in.MinReady:
		return ErrInvalidMaxReadyLessMin
	case in.HeadroomCPUMilli < 0:
		return ErrInvalidHeadroomCPU
	case in.HeadroomMemoryMi < 0:
		return ErrInvalidHeadroomMemory
	default:
		return nil
	}
}

func (p Pool) Validate() error {
	if strings.TrimSpace(p.PlaneID) == "" {
		return ErrPlaneIDRequired
	}
	return UpsertInput{
		MinReady:         p.MinReady,
		MaxReady:         p.MaxReady,
		HeadroomCPUMilli: p.HeadroomCPUMilli,
		HeadroomMemoryMi: p.HeadroomMemoryMi,
	}.Validate()
}

func BuildView(pool Pool, plane plane.Detail) View {
	view := View{
		PlaneID:          pool.PlaneID,
		PlaneName:        plane.Name,
		PlaneDisplayName: plane.DisplayName,
		Provider:         plane.Provider,
		Region:           plane.Region,
		MinReady:         pool.MinReady,
		MaxReady:         pool.MaxReady,
		HeadroomCPUMilli: pool.HeadroomCPUMilli,
		HeadroomMemoryMi: pool.HeadroomMemoryMi,
		CreatedAt:        pool.CreatedAt,
		UpdatedAt:        pool.UpdatedAt,
	}
	if plane.LatestCapacityRecord != nil {
		record := plane.LatestCapacityRecord
		view.Snapshot = SnapshotView{
			NodesTotal:   record.NodesTotal,
			NodesReady:   record.NodesReady,
			CPUMilliFree: freeCapacity(record.CPUMilliCapacity, record.CPUMilliAllocated),
			MemoryMiFree: freeCapacity(record.MemoryMiCapacity, record.MemoryMiAllocated),
		}
		capturedAt := record.CapturedAt
		view.Snapshot.CapturedAt = &capturedAt
	}
	view.Status = buildStatus(pool, plane.LatestCapacityRecord)
	return view
}

func EvaluatePlacement(pool Pool, snapshot *plane.CapacitySnapshot, cpuReq int, memoryReq int) Admission {
	if snapshot == nil {
		return Admission{
			Allowed: false,
			Reason:  "plane does not have a capacity snapshot yet",
		}
	}

	cpuFree := freeCapacity(snapshot.CPUMilliCapacity, snapshot.CPUMilliAllocated)
	memoryFree := freeCapacity(snapshot.MemoryMiCapacity, snapshot.MemoryMiAllocated)
	cpuFreeAfter := cpuFree - cpuReq
	memoryFreeAfter := memoryFree - memoryReq

	if snapshot.NodesReady < pool.MinReady {
		return Admission{
			Allowed:           false,
			Reason:            fmt.Sprintf("plane has raw capacity, but ready runtime nodes=%d is below configured minReady=%d", snapshot.NodesReady, pool.MinReady),
			CPUMilliFreeAfter: cpuFreeAfter,
			MemoryMiFreeAfter: memoryFreeAfter,
		}
	}
	if cpuFreeAfter < pool.HeadroomCPUMilli || memoryFreeAfter < pool.HeadroomMemoryMi {
		return Admission{
			Allowed: false,
			Reason: fmt.Sprintf(
				"plane has raw capacity, but free capacity after selection would be cpu=%dm memory=%dMi, below configured headroom cpu=%dm memory=%dMi",
				cpuFreeAfter,
				memoryFreeAfter,
				pool.HeadroomCPUMilli,
				pool.HeadroomMemoryMi,
			),
			CPUMilliFreeAfter: cpuFreeAfter,
			MemoryMiFreeAfter: memoryFreeAfter,
		}
	}

	return Admission{
		Allowed:           true,
		Reason:            "runtime node pool policy allows this placement",
		CPUMilliFreeAfter: cpuFreeAfter,
		MemoryMiFreeAfter: memoryFreeAfter,
	}
}

func buildStatus(pool Pool, snapshot *plane.CapacitySnapshot) StatusView {
	if snapshot == nil {
		return StatusView{
			Phase:           StatusPhaseSnapshotMiss,
			Reason:          "plane does not have a capacity snapshot yet",
			SnapshotMissing: true,
		}
	}

	cpuFree := freeCapacity(snapshot.CPUMilliCapacity, snapshot.CPUMilliAllocated)
	memoryFree := freeCapacity(snapshot.MemoryMiCapacity, snapshot.MemoryMiAllocated)
	belowMinReady := snapshot.NodesReady < pool.MinReady
	aboveMaxReady := snapshot.NodesReady > pool.MaxReady
	belowHeadroom := cpuFree < pool.HeadroomCPUMilli || memoryFree < pool.HeadroomMemoryMi

	view := StatusView{
		BelowMinReady: belowMinReady,
		AboveMaxReady: aboveMaxReady,
		BelowHeadroom: belowHeadroom,
	}

	switch {
	case belowMinReady:
		view.Phase = StatusPhaseBelowMinReady
		view.Reason = fmt.Sprintf("ready runtime nodes=%d is below configured minReady=%d", snapshot.NodesReady, pool.MinReady)
	case aboveMaxReady:
		view.Phase = StatusPhaseAboveMaxReady
		view.Reason = fmt.Sprintf("ready runtime nodes=%d is above configured maxReady=%d", snapshot.NodesReady, pool.MaxReady)
	case belowHeadroom:
		view.Phase = StatusPhaseBelowHeadroom
		view.Reason = fmt.Sprintf(
			"current free capacity cpu=%dm memory=%dMi is below configured headroom cpu=%dm memory=%dMi",
			cpuFree,
			memoryFree,
			pool.HeadroomCPUMilli,
			pool.HeadroomMemoryMi,
		)
	default:
		view.Phase = StatusPhaseHealthy
		view.Reason = "runtime node pool is healthy"
	}
	return view
}

func freeCapacity(capacity int, allocated int) int {
	if capacity <= allocated {
		return 0
	}
	return capacity - allocated
}
