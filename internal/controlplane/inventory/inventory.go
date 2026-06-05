package inventory

import (
	"sort"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/runtimepool"
)

type Summary struct {
	PlanesTotal           int     `json:"planesTotal"`
	PlanesRegistered      int     `json:"planesRegistered"`
	PlanesRegistering     int     `json:"planesRegistering"`
	PlanesReady           int     `json:"planesReady"`
	PlanesDegraded        int     `json:"planesDegraded"`
	PlanesOffline         int     `json:"planesOffline"`
	PlanesActive          int     `json:"planesActive"`
	PlanesMaintenance     int     `json:"planesMaintenance"`
	PlanesDraining        int     `json:"planesDraining"`
	NodesTotal            int     `json:"nodesTotal"`
	NodesReady            int     `json:"nodesReady"`
	NodesUnavailable      int     `json:"nodesUnavailable"`
	ServicesTotal         int     `json:"servicesTotal"`
	RunsTotal             int     `json:"runsTotal"`
	CPUMilliCapacity      int     `json:"cpuMilliCapacity"`
	CPUMilliAllocated     int     `json:"cpuMilliAllocated"`
	CPUMilliFree          int     `json:"cpuMilliFree"`
	CPUAllocationRatio    float64 `json:"cpuAllocationRatio"`
	MemoryMiCapacity      int     `json:"memoryMiCapacity"`
	MemoryMiAllocated     int     `json:"memoryMiAllocated"`
	MemoryMiFree          int     `json:"memoryMiFree"`
	MemoryAllocationRatio float64 `json:"memoryAllocationRatio"`
	PlanesWithRuntimePool int     `json:"planesWithRuntimePool"`
	PoolsBelowMinReady    int     `json:"poolsBelowMinReady"`
	PoolsBelowHeadroom    int     `json:"poolsBelowHeadroom"`
}

type Group struct {
	Name    string  `json:"name"`
	Summary Summary `json:"summary"`
}

type Plane struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	DisplayName            string     `json:"displayName"`
	Provider               string     `json:"provider"`
	Region                 string     `json:"region"`
	GRPCEndpoint           string     `json:"grpcEndpoint"`
	Registered             bool       `json:"registered"`
	Status                 string     `json:"status"`
	StatusMessage          string     `json:"statusMessage"`
	OperationState         string     `json:"operationState"`
	OperationReason        string     `json:"operationReason"`
	OperationUpdatedAt     *time.Time `json:"operationUpdatedAt,omitempty"`
	AcceptingNewRuns       bool       `json:"acceptingNewRuns"`
	LastHeartbeatAt        *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSyncAt             *time.Time `json:"lastSyncAt,omitempty"`
	CapacityCapturedAt     *time.Time `json:"capacityCapturedAt,omitempty"`
	NodesTotal             int        `json:"nodesTotal"`
	NodesReady             int        `json:"nodesReady"`
	NodesUnavailable       int        `json:"nodesUnavailable"`
	ServicesTotal          int        `json:"servicesTotal"`
	RunsTotal              int        `json:"runsTotal"`
	CPUMilliCapacity       int        `json:"cpuMilliCapacity"`
	CPUMilliAllocated      int        `json:"cpuMilliAllocated"`
	CPUMilliFree           int        `json:"cpuMilliFree"`
	MemoryMiCapacity       int        `json:"memoryMiCapacity"`
	MemoryMiAllocated      int        `json:"memoryMiAllocated"`
	MemoryMiFree           int        `json:"memoryMiFree"`
	RuntimePoolConfigured  bool       `json:"runtimePoolConfigured"`
	RuntimePoolPhase       string     `json:"runtimePoolPhase,omitempty"`
	RuntimePoolReason      string     `json:"runtimePoolReason,omitempty"`
	RuntimePoolMinReady    int        `json:"runtimePoolMinReady,omitempty"`
	RuntimePoolMaxReady    int        `json:"runtimePoolMaxReady,omitempty"`
	RuntimePoolHeadroomCPU int        `json:"runtimePoolHeadroomCPUMilli,omitempty"`
	RuntimePoolHeadroomMem int        `json:"runtimePoolHeadroomMemoryMi,omitempty"`
}

type View struct {
	Summary   Summary `json:"summary"`
	Providers []Group `json:"providers"`
	Regions   []Group `json:"regions"`
	Planes    []Plane `json:"planes"`
}

func Build(items []plane.Detail, pools []runtimepool.Pool) View {
	view := View{
		Providers: make([]Group, 0),
		Regions:   make([]Group, 0),
		Planes:    make([]Plane, 0, len(items)),
	}

	providerGroups := make(map[string]*Summary)
	regionGroups := make(map[string]*Summary)
	poolByPlane := make(map[string]runtimepool.Pool, len(pools))
	for _, item := range pools {
		poolByPlane[item.PlaneID] = item
	}

	for _, item := range items {
		planeView := buildPlane(item, poolByPlane[item.ID])
		view.Planes = append(view.Planes, planeView)
		accumulateSummary(&view.Summary, planeView)

		providerSummary := providerGroups[planeView.Provider]
		if providerSummary == nil {
			providerSummary = &Summary{}
			providerGroups[planeView.Provider] = providerSummary
		}
		accumulateSummary(providerSummary, planeView)

		regionSummary := regionGroups[planeView.Region]
		if regionSummary == nil {
			regionSummary = &Summary{}
			regionGroups[planeView.Region] = regionSummary
		}
		accumulateSummary(regionSummary, planeView)
	}

	finalizeSummary(&view.Summary)
	for name, summary := range providerGroups {
		finalizeSummary(summary)
		view.Providers = append(view.Providers, Group{Name: name, Summary: *summary})
	}
	for name, summary := range regionGroups {
		finalizeSummary(summary)
		view.Regions = append(view.Regions, Group{Name: name, Summary: *summary})
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

func buildPlane(item plane.Detail, pool runtimepool.Pool) Plane {
	plane := Plane{
		ID:               item.ID,
		Name:             item.Name,
		DisplayName:      item.DisplayName,
		Provider:         item.Provider,
		Region:           item.Region,
		GRPCEndpoint:     item.GRPCEndpoint,
		Registered:       item.Registration.Registered,
		Status:           string(item.Status.Status),
		StatusMessage:    item.Status.Message,
		OperationState:   string(item.Operation.ResolvedState()),
		OperationReason:  item.Operation.Reason,
		AcceptingNewRuns: item.Operation.AcceptingNewRuns(),
		LastHeartbeatAt:  item.Status.LastHeartbeatAt,
		LastSyncAt:       item.Status.LastSyncAt,
	}
	if !item.Operation.UpdatedAt.IsZero() {
		updatedAt := item.Operation.UpdatedAt
		plane.OperationUpdatedAt = &updatedAt
	}

	if item.LatestCapacityRecord != nil {
		record := item.LatestCapacityRecord
		plane.NodesTotal = record.NodesTotal
		plane.NodesReady = record.NodesReady
		plane.NodesUnavailable = unavailableNodes(record.NodesTotal, record.NodesReady)
		plane.ServicesTotal = record.ServicesTotal
		plane.RunsTotal = record.RunsTotal
		plane.CPUMilliCapacity = record.CPUMilliCapacity
		plane.CPUMilliAllocated = record.CPUMilliAllocated
		plane.CPUMilliFree = freeCapacity(record.CPUMilliCapacity, record.CPUMilliAllocated)
		plane.MemoryMiCapacity = record.MemoryMiCapacity
		plane.MemoryMiAllocated = record.MemoryMiAllocated
		plane.MemoryMiFree = freeCapacity(record.MemoryMiCapacity, record.MemoryMiAllocated)
		capturedAt := record.CapturedAt
		plane.CapacityCapturedAt = &capturedAt
	}
	if pool.PlaneID != "" {
		poolView := runtimepool.BuildView(pool, item)
		plane.RuntimePoolConfigured = true
		plane.RuntimePoolPhase = string(poolView.Status.Phase)
		plane.RuntimePoolReason = poolView.Status.Reason
		plane.RuntimePoolMinReady = poolView.MinReady
		plane.RuntimePoolMaxReady = poolView.MaxReady
		plane.RuntimePoolHeadroomCPU = poolView.HeadroomCPUMilli
		plane.RuntimePoolHeadroomMem = poolView.HeadroomMemoryMi
	}

	return plane
}

func accumulateSummary(summary *Summary, planeView Plane) {
	summary.PlanesTotal++
	if planeView.Registered {
		summary.PlanesRegistered++
	}

	switch planeView.Status {
	case string(plane.StatusRegistering):
		summary.PlanesRegistering++
	case string(plane.StatusReady):
		summary.PlanesReady++
	case string(plane.StatusDegraded):
		summary.PlanesDegraded++
	case string(plane.StatusOffline):
		summary.PlanesOffline++
	}
	switch planeView.OperationState {
	case string(plane.OperationStateActive):
		summary.PlanesActive++
	case string(plane.OperationStateMaintenance):
		summary.PlanesMaintenance++
	case string(plane.OperationStateDraining):
		summary.PlanesDraining++
	}

	summary.NodesTotal += planeView.NodesTotal
	summary.NodesReady += planeView.NodesReady
	summary.NodesUnavailable += planeView.NodesUnavailable
	summary.ServicesTotal += planeView.ServicesTotal
	summary.RunsTotal += planeView.RunsTotal
	summary.CPUMilliCapacity += planeView.CPUMilliCapacity
	summary.CPUMilliAllocated += planeView.CPUMilliAllocated
	summary.MemoryMiCapacity += planeView.MemoryMiCapacity
	summary.MemoryMiAllocated += planeView.MemoryMiAllocated
	if planeView.RuntimePoolConfigured {
		summary.PlanesWithRuntimePool++
		if planeView.RuntimePoolPhase == string(runtimepool.StatusPhaseBelowMinReady) {
			summary.PoolsBelowMinReady++
		}
		if planeView.RuntimePoolPhase == string(runtimepool.StatusPhaseBelowHeadroom) {
			summary.PoolsBelowHeadroom++
		}
	}
}

func finalizeSummary(summary *Summary) {
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
