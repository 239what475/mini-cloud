package api

import (
	"sort"
	"time"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

type inventorySummary struct {
	PlanesTotal           int     `json:"planesTotal"`
	PlanesSyncing         int     `json:"planesSyncing"`
	PlanesReady           int     `json:"planesReady"`
	PlanesDegraded        int     `json:"planesDegraded"`
	PlanesOffline         int     `json:"planesOffline"`
	NodesTotal            int     `json:"nodesTotal"`
	NodesReady            int     `json:"nodesReady"`
	NodesUnavailable      int     `json:"nodesUnavailable"`
	CPUMilliCapacity      int     `json:"cpuMilliCapacity"`
	CPUMilliAllocated     int     `json:"cpuMilliAllocated"`
	CPUMilliFree          int     `json:"cpuMilliFree"`
	CPUAllocationRatio    float64 `json:"cpuAllocationRatio"`
	MemoryMiCapacity      int     `json:"memoryMiCapacity"`
	MemoryMiAllocated     int     `json:"memoryMiAllocated"`
	MemoryMiFree          int     `json:"memoryMiFree"`
	MemoryAllocationRatio float64 `json:"memoryAllocationRatio"`
}

type inventoryGroup struct {
	Name    string           `json:"name"`
	Summary inventorySummary `json:"summary"`
}

type inventoryPlane struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	DisplayName        string     `json:"displayName"`
	Provider           string     `json:"provider"`
	Region             string     `json:"region"`
	GRPCEndpoint       string     `json:"grpcEndpoint"`
	Status             string     `json:"status"`
	StatusMessage      string     `json:"statusMessage"`
	LastHeartbeatAt    *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt         *time.Time `json:"lastSyncAt,omitempty"`
	CapacityCapturedAt *time.Time `json:"capacityCapturedAt,omitempty"`
	NodesTotal         int        `json:"nodesTotal"`
	NodesReady         int        `json:"nodesReady"`
	NodesUnavailable   int        `json:"nodesUnavailable"`
	CPUMilliCapacity   int        `json:"cpuMilliCapacity"`
	CPUMilliAllocated  int        `json:"cpuMilliAllocated"`
	CPUMilliFree       int        `json:"cpuMilliFree"`
	MemoryMiCapacity   int        `json:"memoryMiCapacity"`
	MemoryMiAllocated  int        `json:"memoryMiAllocated"`
	MemoryMiFree       int        `json:"memoryMiFree"`
}

type inventoryView struct {
	Summary   inventorySummary `json:"summary"`
	Providers []inventoryGroup `json:"providers"`
	Regions   []inventoryGroup `json:"regions"`
	Planes    []inventoryPlane `json:"planes"`
}

func buildInventoryView(items []coordination.PlaneSnapshotView) inventoryView {
	view := inventoryView{
		Providers: make([]inventoryGroup, 0),
		Regions:   make([]inventoryGroup, 0),
		Planes:    make([]inventoryPlane, 0, len(items)),
	}

	providerGroups := make(map[string]*inventorySummary)
	regionGroups := make(map[string]*inventorySummary)

	for _, item := range items {
		planeView := buildPlane(item)
		view.Planes = append(view.Planes, planeView)
		accumulateSummary(&view.Summary, planeView)

		providerSummary := providerGroups[planeView.Provider]
		if providerSummary == nil {
			providerSummary = &inventorySummary{}
			providerGroups[planeView.Provider] = providerSummary
		}
		accumulateSummary(providerSummary, planeView)

		regionSummary := regionGroups[planeView.Region]
		if regionSummary == nil {
			regionSummary = &inventorySummary{}
			regionGroups[planeView.Region] = regionSummary
		}
		accumulateSummary(regionSummary, planeView)
	}

	finalizeSummary(&view.Summary)
	for name, summary := range providerGroups {
		finalizeSummary(summary)
		view.Providers = append(view.Providers, inventoryGroup{Name: name, Summary: *summary})
	}
	for name, summary := range regionGroups {
		finalizeSummary(summary)
		view.Regions = append(view.Regions, inventoryGroup{Name: name, Summary: *summary})
	}

	sort.Slice(view.Providers, func(i, j int) bool {
		return view.Providers[i].Name < view.Providers[j].Name
	})
	sort.Slice(view.Regions, func(i, j int) bool {
		return view.Regions[i].Name < view.Regions[j].Name
	})
	sort.Slice(view.Planes, func(i, j int) bool {
		if view.Planes[i].Provider != view.Planes[j].Provider {
			return view.Planes[i].Provider < view.Planes[j].Provider
		}
		if view.Planes[i].Region != view.Planes[j].Region {
			return view.Planes[i].Region < view.Planes[j].Region
		}
		if view.Planes[i].Name != view.Planes[j].Name {
			return view.Planes[i].Name < view.Planes[j].Name
		}
		return view.Planes[i].ID < view.Planes[j].ID
	})

	return view
}

func buildPlane(item coordination.PlaneSnapshotView) inventoryPlane {
	plane := inventoryPlane{
		ID:              item.Plane.ID,
		Name:            item.Plane.Name,
		DisplayName:     item.Plane.DisplayName,
		Provider:        item.Plane.Provider,
		Region:          item.Plane.Region,
		GRPCEndpoint:    item.Plane.GRPCEndpoint,
		Status:          item.Plane.Status.Status,
		StatusMessage:   item.Plane.Status.Message,
		LastHeartbeatAt: item.Plane.Status.LastHeartbeatAt,
		LastSyncAt:      item.Plane.Status.LastSyncAt,
	}

	if item.Snapshot != nil {
		record := nodeInventoryFromSnapshot(item.Plane.ID, item.Snapshot)
		plane.NodesTotal = record.NodesTotal
		plane.NodesReady = record.NodesReady
		plane.NodesUnavailable = unavailableNodes(record.NodesTotal, record.NodesReady)
		plane.CPUMilliCapacity = record.CPUMilliCapacity
		plane.CPUMilliAllocated = record.CPUMilliAllocated
		plane.CPUMilliFree = freeCapacity(record.CPUMilliCapacity, record.CPUMilliAllocated)
		plane.MemoryMiCapacity = record.MemoryMiCapacity
		plane.MemoryMiAllocated = record.MemoryMiAllocated
		plane.MemoryMiFree = freeCapacity(record.MemoryMiCapacity, record.MemoryMiAllocated)
		capturedAt := record.ObservedAt
		plane.CapacityCapturedAt = &capturedAt
	}

	return plane
}

func nodeInventoryFromSnapshot(planeID string, snapshot *cloudplanev1.PlaneSnapshot) model.NodeInventorySnapshot {
	nodeInventory := snapshot.GetNodeInventory()
	out := model.NodeInventorySnapshot{
		PlaneID:    planeID,
		ObservedAt: time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if observedAt := nodeInventory.GetObservedAt(); observedAt != nil {
		out.ObservedAt = observedAt.AsTime().UTC()
	}
	for _, item := range nodeInventory.GetNodes() {
		if item == nil {
			continue
		}
		out.NodesTotal++
		if item.GetStatus() == "ready" {
			out.NodesReady++
		}
		out.CPUMilliCapacity += int(item.GetCpuMilliAllocatable())
		out.CPUMilliAllocated += int(item.GetCpuMilliAllocated())
		out.MemoryMiCapacity += int(item.GetMemoryMiAllocatable())
		out.MemoryMiAllocated += int(item.GetMemoryMiAllocated())
	}
	return out
}

func accumulateSummary(summary *inventorySummary, planeView inventoryPlane) {
	summary.PlanesTotal++

	switch planeView.Status {
	case model.StatusSyncing:
		summary.PlanesSyncing++
	case model.StatusReady:
		summary.PlanesReady++
	case model.StatusDegraded:
		summary.PlanesDegraded++
	case model.StatusOffline:
		summary.PlanesOffline++
	}
	summary.NodesTotal += planeView.NodesTotal
	summary.NodesReady += planeView.NodesReady
	summary.NodesUnavailable += planeView.NodesUnavailable
	summary.CPUMilliCapacity += planeView.CPUMilliCapacity
	summary.CPUMilliAllocated += planeView.CPUMilliAllocated
	summary.MemoryMiCapacity += planeView.MemoryMiCapacity
	summary.MemoryMiAllocated += planeView.MemoryMiAllocated
}

func finalizeSummary(summary *inventorySummary) {
	summary.CPUMilliFree = freeCapacity(summary.CPUMilliCapacity, summary.CPUMilliAllocated)
	summary.MemoryMiFree = freeCapacity(summary.MemoryMiCapacity, summary.MemoryMiAllocated)
	summary.CPUAllocationRatio = utilizationRatio(summary.CPUMilliAllocated, summary.CPUMilliCapacity)
	summary.MemoryAllocationRatio = utilizationRatio(summary.MemoryMiAllocated, summary.MemoryMiCapacity)
}

func unavailableNodes(total int, ready int) int {
	if total <= ready {
		return 0
	}
	return total - ready
}

func freeCapacity(capacity int, allocated int) int {
	if capacity <= allocated {
		return 0
	}
	return capacity - allocated
}

func utilizationRatio(allocated int, capacity int) float64 {
	if capacity <= 0 {
		return 0
	}
	return float64(allocated) / float64(capacity)
}
