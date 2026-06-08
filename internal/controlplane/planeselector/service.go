package planeselector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/store"
)

type Selector struct {
	logger *slog.Logger
	store  *store.Store
}

func NewSelector(logger *slog.Logger, stores *store.Store) *Selector {
	return &Selector{
		logger: logger,
		store:  stores,
	}
}

func (s *Selector) PreviewSelection(ctx context.Context, input SelectionInput) (SelectionResult, error) {
	if s == nil || s.store == nil {
		return SelectionResult{}, fmt.Errorf("plane selector is not configured")
	}
	if err := input.Validate(); err != nil {
		return SelectionResult{}, err
	}

	cpuReq, memoryReq, err := input.ResourceRequest()
	if err != nil {
		return SelectionResult{}, err
	}

	planes, err := s.store.ListPlanes(ctx)
	if err != nil {
		return SelectionResult{}, err
	}
	excludedPlaneIDs := make(map[string]struct{}, len(input.ExcludePlaneIDs))
	for _, planeID := range input.ExcludePlaneIDs {
		if trimmed := strings.TrimSpace(planeID); trimmed != "" {
			excludedPlaneIDs[trimmed] = struct{}{}
		}
	}

	result := SelectionResult{Candidates: make([]Candidate, 0, len(planes))}
	var registeredMatched int
	var readyMatched int
	var operationMatched int
	var providerMatched int
	var regionMatched int
	var rawCapacityMatched int
	var best *Decision
	bestIndex := -1

	for _, planeDetail := range planes {
		candidate := Candidate{
			PlaneID:          planeDetail.ID,
			PlaneName:        planeDetail.Name,
			PlaneDisplayName: planeDetail.DisplayName,
			Provider:         planeDetail.Provider,
			Region:           planeDetail.Region,
			Registered:       planeDetail.Registration.Registered,
			Status:           string(planeDetail.Status.Status),
			OperationState:   string(planeDetail.Operation.ResolvedState()),
			AcceptingNewRuns: planeDetail.Operation.AcceptingNewRuns(),
		}

		if !planeDetail.Registration.Registered {
			result.FilteredCounts.Registration++
			candidate.Reason = "plane southbound registration is not complete"
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		registeredMatched++

		if planeDetail.Status.Status != plane.StatusReady {
			result.FilteredCounts.Status++
			candidate.Reason = fmt.Sprintf("plane status is %s, not ready", planeDetail.Status.Status)
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		readyMatched++

		if !planeDetail.Operation.AcceptingNewRuns() {
			result.FilteredCounts.Operation++
			candidate.Reason = fmt.Sprintf("plane operation state is %s, so it is not accepting new runs", planeDetail.Operation.ResolvedState())
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		operationMatched++

		if pinned := strings.TrimSpace(input.PinnedPlaneID); pinned != "" && planeDetail.ID != pinned {
			result.FilteredCounts.PinnedPlane++
			candidate.Reason = fmt.Sprintf("plane id %s does not match pinned plane %s", planeDetail.ID, pinned)
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		if planeDetail.Provider != input.Provider {
			result.FilteredCounts.Provider++
			candidate.Reason = fmt.Sprintf("plane provider %s does not match requested provider %s", planeDetail.Provider, input.Provider)
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		providerMatched++

		if planeDetail.Region != input.Region {
			result.FilteredCounts.Region++
			candidate.Reason = fmt.Sprintf("plane region %s does not match requested region %s", planeDetail.Region, input.Region)
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		regionMatched++

		if _, excluded := excludedPlaneIDs[planeDetail.ID]; excluded {
			result.FilteredCounts.AntiAffinity++
			candidate.Reason = "plane is excluded by the selection request"
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		if planeDetail.LatestRuntimeInventory == nil {
			result.FilteredCounts.Capacity++
			candidate.Reason = "plane does not have a runtime inventory snapshot yet"
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		inventory := planeDetail.LatestRuntimeInventory
		cpuFree := inventory.CPUMilliCapacity - inventory.CPUMilliAllocated
		memFree := inventory.MemoryMiCapacity - inventory.MemoryMiAllocated
		candidate.CPUMilliFree = nonNegative(cpuFree)
		candidate.MemoryMiFree = nonNegative(memFree)
		candidate.BasedOnInventorySyncVersion = planeDetail.LatestRuntimeInventory.SyncVersion
		if candidate.CPUMilliFree < cpuReq || candidate.MemoryMiFree < memoryReq {
			result.FilteredCounts.Capacity++
			candidate.Reason = fmt.Sprintf(
				"plane free capacity is cpu=%dm memory=%dMi, below requested total cpu=%dm memory=%dMi",
				candidate.CPUMilliFree,
				candidate.MemoryMiFree,
				cpuReq,
				memoryReq,
			)
			result.Candidates = append(result.Candidates, candidate)
			continue
		}
		rawCapacityMatched++

		candidate.CPUMilliFreeAfter = candidate.CPUMilliFree - cpuReq
		candidate.MemoryMiFreeAfter = candidate.MemoryMiFree - memoryReq

		candidate.Eligible = true
		candidate.Score = score(candidate.CPUMilliFreeAfter, candidate.MemoryMiFreeAfter)
		candidate.Reason = fmt.Sprintf(
			"selected plane candidate because it has enough aggregate runtime capacity in provider %s region %s",
			input.Provider,
			input.Region,
		)
		result.Candidates = append(result.Candidates, candidate)

		currentDecision := Decision{
			PlaneID:                     planeDetail.ID,
			PlaneName:                   planeDetail.Name,
			PlaneDisplayName:            planeDetail.DisplayName,
			Provider:                    planeDetail.Provider,
			Region:                      planeDetail.Region,
			BasedOnInventorySyncVersion: planeDetail.LatestRuntimeInventory.SyncVersion,
			Score:                       candidate.Score,
			Reason: fmt.Sprintf(
				"selected plane %s because it has the highest remaining aggregate cpu/memory headroom in provider %s region %s",
				planeDetail.Name,
				input.Provider,
				input.Region,
			),
		}
		if best == nil || better(currentDecision, *best) {
			copyDecision := currentDecision
			best = &copyDecision
			bestIndex = len(result.Candidates) - 1
		}
	}

	if best == nil {
		result.FailureReason = buildFailureReason(registeredMatched, readyMatched, operationMatched, providerMatched, regionMatched, rawCapacityMatched, result.FilteredCounts, input)
		return result, nil
	}

	result.Decision = best
	if bestIndex >= 0 && bestIndex < len(result.Candidates) {
		result.Candidates[bestIndex].Selected = true
		result.Candidates[bestIndex].Reason = best.Reason
	}
	return result, nil
}

func better(current Decision, best Decision) bool {
	if current.Score != best.Score {
		return current.Score > best.Score
	}
	return current.PlaneID < best.PlaneID
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
