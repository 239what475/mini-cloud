package planeselector

import (
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/controlplane/deploy"
)

var (
	ErrProjectIDRequired = errors.New("projectID is required")
	ErrProviderRequired  = errors.New("provider is required")
	ErrRegionRequired    = errors.New("region is required")
)

type SelectionInput struct {
	Provider        string   `json:"provider"`
	Region          string   `json:"region"`
	PinnedPlaneID   string   `json:"pinnedPlaneID,omitempty"`
	ExcludePlaneIDs []string `json:"excludePlaneIDs,omitempty"`
	InstanceClass   string   `json:"instanceClass"`
	Replicas        int      `json:"replicas"`
}

type ApplyServiceInput struct {
	Metadata      deploy.ServiceMetadata `json:"metadata"`
	Provider      string                 `json:"provider"`
	PinnedPlaneID string                 `json:"pinnedPlaneID,omitempty"`
	Spec          deploy.ServiceSpec     `json:"spec"`
}

type FilteredCounts struct {
	Registration int `json:"registration"`
	Status       int `json:"status"`
	Operation    int `json:"operation"`
	PinnedPlane  int `json:"pinnedPlane"`
	Provider     int `json:"provider"`
	Region       int `json:"region"`
	AntiAffinity int `json:"antiAffinity"`
	Capacity     int `json:"capacity"`
	PoolMinReady int `json:"poolMinReady"`
	Headroom     int `json:"headroom"`
}

type Candidate struct {
	PlaneID                     string `json:"planeID"`
	PlaneName                   string `json:"planeName"`
	PlaneDisplayName            string `json:"planeDisplayName"`
	Provider                    string `json:"provider"`
	Region                      string `json:"region"`
	Registered                  bool   `json:"registered"`
	Status                      string `json:"status"`
	OperationState              string `json:"operationState"`
	ProjectID                   string `json:"projectID,omitempty"`
	AcceptingNewDeployments     bool   `json:"acceptingNewDeployments"`
	CPUMilliFree                int    `json:"cpuMilliFree"`
	MemoryMiFree                int    `json:"memoryMiFree"`
	CPUMilliFreeAfter           int    `json:"cpuMilliFreeAfter"`
	MemoryMiFreeAfter           int    `json:"memoryMiFreeAfter"`
	BasedOnInventorySyncVersion int64  `json:"basedOnInventorySyncVersion,omitempty"`
	Eligible                    bool   `json:"eligible"`
	Selected                    bool   `json:"selected"`
	Score                       int64  `json:"score,omitempty"`
	Reason                      string `json:"reason"`
}

type Decision struct {
	PlaneID                     string `json:"planeID"`
	PlaneName                   string `json:"planeName"`
	PlaneDisplayName            string `json:"planeDisplayName"`
	Provider                    string `json:"provider"`
	Region                      string `json:"region"`
	ProjectID                   string `json:"projectID"`
	BasedOnInventorySyncVersion int64  `json:"basedOnInventorySyncVersion"`
	Score                       int64  `json:"score"`
	Reason                      string `json:"reason"`
}

type SelectionResult struct {
	Decision       *Decision      `json:"decision"`
	FailureReason  string         `json:"failureReason"`
	FilteredCounts FilteredCounts `json:"filteredCounts"`
	Candidates     []Candidate    `json:"candidates"`
}

type ApplyResult struct {
	Selection SelectionResult    `json:"selection"`
	Accepted  deploy.ApplyResult `json:"accepted"`
}

func (in SelectionInput) Validate() error {
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	if strings.TrimSpace(in.Region) == "" {
		return ErrRegionRequired
	}
	if in.Replicas <= 0 {
		return deploy.ErrInvalidReplicas
	}
	if !deploy.IsInstanceClass(in.InstanceClass) {
		return deploy.ErrInvalidInstanceClass
	}
	return nil
}

func (in SelectionInput) ResourceRequest() (cpuMilli int, memoryMi int, err error) {
	if err := in.Validate(); err != nil {
		return 0, 0, err
	}
	cpuMilli, memoryMi, err = deploy.ResourceRequest(in.InstanceClass)
	if err != nil {
		return 0, 0, err
	}
	return cpuMilli * in.Replicas, memoryMi * in.Replicas, nil
}

func (in ApplyServiceInput) SelectionInput() SelectionInput {
	return SelectionInput{
		Provider:      in.Provider,
		Region:        in.Spec.Region,
		PinnedPlaneID: in.PinnedPlaneID,
		InstanceClass: in.Spec.InstanceClass,
		Replicas:      in.Spec.Replicas,
	}
}

func (in ApplyServiceInput) Validate(projectID string) error {
	if strings.TrimSpace(in.Provider) == "" {
		return ErrProviderRequired
	}
	deployInput := deploy.ApplyServiceInput{
		Metadata: in.Metadata,
		Spec:     in.Spec,
	}
	deployInput.Metadata.ProjectID = projectID
	return deployInput.Validate()
}

func (in ApplyServiceInput) ToDeployInput(projectID string, region string) deploy.ApplyServiceInput {
	out := deploy.ApplyServiceInput{
		Metadata: in.Metadata,
		Spec:     in.Spec,
	}
	out.Metadata.ProjectID = projectID
	out.Spec.Region = region
	return out
}

func buildFailureReason(registeredMatched int, readyMatched int, operationMatched int, providerMatched int, regionMatched int, rawCapacityMatched int, filtered FilteredCounts, input SelectionInput) string {
	switch {
	case registeredMatched == 0:
		return "no planes have completed bootstrap registration"
	case readyMatched == 0:
		return "no registered planes are currently ready"
	case operationMatched == 0:
		return "no ready planes are currently accepting new deployments"
	case strings.TrimSpace(input.PinnedPlaneID) != "" && providerMatched == 0:
		return "the pinned plane is not ready"
	case strings.TrimSpace(input.PinnedPlaneID) != "" && regionMatched == 0:
		return "the pinned plane does not satisfy the requested provider/region"
	case providerMatched == 0:
		return fmt.Sprintf("no ready planes matched provider %s", input.Provider)
	case regionMatched == 0:
		return fmt.Sprintf("no ready planes matched region %s", input.Region)
	case rawCapacityMatched == 0:
		return "ready planes matched provider/region, but none had enough free cpu/memory"
	case filtered.PoolMinReady > 0 && filtered.PoolMinReady == rawCapacityMatched:
		return "ready planes had enough raw cpu/memory, but none currently satisfy runtime node pool minReady"
	case filtered.Headroom > 0 && filtered.Headroom == rawCapacityMatched:
		return "ready planes had enough raw cpu/memory, but none could preserve runtime node pool headroom"
	case filtered.PoolMinReady > 0 || filtered.Headroom > 0:
		return "ready planes had enough raw cpu/memory, but runtime node pool policy blocked admission"
	default:
		return "ready planes matched provider/region, but none had enough free cpu/memory"
	}
}

func score(cpuFreeAfter int, memoryFreeAfter int) int64 {
	return int64(cpuFreeAfter)*1_000_000 + int64(memoryFreeAfter)
}
