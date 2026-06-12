package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	planeExecutionStatusFailed    = "failed"
	planeExecutionStatusRunning   = "running"
	planeExecutionStatusSucceeded = "succeeded"
	RequestPlaneSyncTimeout       = 10 * time.Second
)

type PlaneSyncer struct {
	logger          *slog.Logger
	store           *store.Store
	southboundToken string
	dns             dnsClient
	now             func() time.Time
}

type syncOverview struct {
	NodesTotal          int
	NodesReady          int
	NodesDraining       int
	NodesOffline        int
	ExecutionPlansTotal int
}

type syncError struct {
	status  string
	message string
}

func (e *syncError) Error() string {
	return e.message
}

func NewPlaneSyncer(logger *slog.Logger, stores *store.Store, southboundToken string, dns dnsClient) *PlaneSyncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaneSyncer{
		logger:          logger,
		store:           stores,
		southboundToken: strings.TrimSpace(southboundToken),
		dns:             dns,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *PlaneSyncer) SyncPlane(ctx context.Context, planeID string) error {
	logger := s.logger.With("plane_id", planeID)
	planeDetail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return err
	}

	grpcEndpoint := strings.TrimRight(strings.TrimSpace(planeDetail.GRPCEndpoint), "/")
	client, err := newPlaneClient(grpcEndpoint, s.southboundToken)
	if err != nil {
		syncErr := &syncError{
			status:  model.StatusOffline,
			message: fmt.Sprintf("initialize plane southbound client failed: %v", err),
		}
		syncedAt := s.now()
		if updateErr := s.store.UpdatePlaneStatus(ctx, planeID, store.UpdatePlaneStatusInput{
			Status:     syncErr.status,
			Message:    syncErr.message,
			LastSyncAt: &syncedAt,
		}); updateErr != nil {
			logger.Error("update failed plane status failed", "error", updateErr)
		}
		return syncErr
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			logger.Warn("close plane southbound client failed", "error", closeErr)
		}
	}()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		syncErr := &syncError{
			status:  model.StatusOffline,
			message: fmt.Sprintf("load plane snapshot failed: %v", err),
		}
		syncedAt := s.now()
		if updateErr := s.store.UpdatePlaneStatus(ctx, planeID, store.UpdatePlaneStatusInput{
			Status:     syncErr.status,
			Message:    syncErr.message,
			LastSyncAt: &syncedAt,
		}); updateErr != nil {
			logger.Error("update failed plane status failed", "error", updateErr)
		}
		return syncErr
	}
	checkedAt := protoTime(snapshot.GetCheckedAt())

	syncedAt := s.now()
	status, message := derivePlaneStatus(planeDetail, snapshot)
	if err := s.store.UpdatePlaneStatus(ctx, planeID, store.UpdatePlaneStatusInput{
		Status:          status,
		Message:         message,
		LastHeartbeatAt: &checkedAt,
		LastSyncAt:      &syncedAt,
	}); err != nil {
		return err
	}
	if err := s.store.ReplacePlaneNodeInventory(ctx, planeID, buildNodeInventory(snapshot)); err != nil {
		return err
	}
	if err := s.applyServiceSnapshots(ctx, planeID, checkedAt, snapshot.GetServices()); err != nil {
		return err
	}
	if err := s.applyExecutionSnapshots(ctx, planeID, snapshot.GetExecutions(), snapshot.GetFrontdoorDomains()); err != nil {
		return err
	}
	if err := s.applyFrontDoorDNS(ctx, planeID, snapshot.GetFrontdoorDomains()); err != nil {
		return err
	}
	return nil
}

func (s *PlaneSyncer) SyncRegisteredPlanes(ctx context.Context, perPlaneTimeout time.Duration) error {
	planeIDs, err := s.store.ListPlaneIDs(ctx)
	if err != nil {
		return err
	}

	for _, planeID := range planeIDs {
		planeCtx := ctx
		cancel := func() {}
		if perPlaneTimeout > 0 {
			planeCtx, cancel = context.WithTimeout(ctx, perPlaneTimeout)
		}
		err := s.SyncPlane(planeCtx, planeID)
		cancel()
		if err != nil {
			s.logger.Warn("plane sync failed", "plane_id", planeID, "error", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (s *PlaneSyncer) applyExecutionSnapshots(ctx context.Context, planeID string, executions []*cloudplanev1.PlaneExecutionSnapshot, frontdoorDomains []*cloudplanev1.PlaneFrontDoorDomain) error {
	activeFrontDoorHosts := frontDoorHosts(frontdoorDomains)
	for _, item := range executions {
		if item == nil || strings.TrimSpace(item.GetServiceId()) == "" || item.GetServiceGeneration() <= 0 {
			continue
		}
		serviceItem, err := s.store.GetService(ctx, item.GetServiceId())
		if err != nil {
			if errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
		if strings.TrimSpace(serviceItem.Spec.PlaneID) != planeID {
			continue
		}
		if item.GetServiceGeneration() != serviceItem.Metadata.Generation {
			continue
		}
		if serviceItem.Status.DesiredState == model.DesiredStateDeleted {
			if strings.TrimSpace(item.GetStatus()) == planeExecutionStatusSucceeded {
				if _, ok := activeFrontDoorHosts[cleanSyncDomain(serviceItem.Metadata.Host)]; ok {
					continue
				}
				if err := s.deleteServiceDNS(ctx, serviceItem); err != nil {
					return err
				}
				if err := s.store.DeleteServiceForGeneration(ctx, item.GetServiceId(), item.GetServiceGeneration()); err != nil &&
					!errors.Is(err, store.ErrServiceNotFound) &&
					!errors.Is(err, store.ErrServiceGenerationConflict) {
					return err
				}
			}
			continue
		}
		status := serviceStatusFromExecutionSnapshot(serviceItem, item)
		if err := s.store.UpdateServiceStatusForGeneration(ctx, item.GetServiceId(), item.GetServiceGeneration(), store.UpdateServiceStatusInput{
			ObservedGeneration: status.Observed.ObservedGeneration,
			Phase:              status.Observed.Phase,
			Message:            status.Observed.Message,
			LastObservedAt:     status.Observed.LastObservedAt,
			Run:                &status.Run,
		}); err != nil {
			if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

func frontDoorHosts(domains []*cloudplanev1.PlaneFrontDoorDomain) map[string]struct{} {
	hosts := make(map[string]struct{})
	for _, item := range domains {
		if item == nil {
			continue
		}
		host := cleanSyncDomain(item.GetHost())
		if host == "" {
			continue
		}
		hosts[host] = struct{}{}
	}
	return hosts
}

func cleanSyncDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func (s *PlaneSyncer) applyServiceSnapshots(ctx context.Context, planeID string, observedAt time.Time, services []*cloudplanev1.PlaneService) error {
	for _, item := range services {
		if item == nil || strings.TrimSpace(item.GetServiceId()) == "" {
			continue
		}
		if err := s.store.UpsertServiceSnapshot(ctx, store.UpsertServiceSnapshotInput{
			PlaneID:       planeID,
			ServiceID:     item.GetServiceId(),
			Name:          item.GetName(),
			Host:          item.GetHost(),
			Generation:    item.GetGeneration(),
			DesiredState:  item.GetDesiredState(),
			ObservedAt:    observedAt,
			StatusMessage: "observed from cloud-plane snapshot",
		}); err != nil {
			return err
		}
	}
	return nil
}

type executionDerivedStatus struct {
	Observed model.ServiceObservedStatus
	Run      model.RunStatus
}

func serviceStatusFromExecutionSnapshot(serviceItem model.Service, item *cloudplanev1.PlaneExecutionSnapshot) executionDerivedStatus {
	now := protoTime(item.GetObservedAt())
	if now.IsZero() {
		now = time.Now().UTC()
	}
	message := executionSnapshotMessage(item)
	phase := model.PhaseProgressing
	runPhase := model.RunPhaseDispatching

	switch strings.TrimSpace(item.GetStatus()) {
	case planeExecutionStatusFailed:
		phase = model.PhaseDegraded
		runPhase = model.RunPhaseFailed
	case planeExecutionStatusRunning:
		phase = model.PhaseReady
		runPhase = model.RunPhaseRunning
	}
	runStatus := model.RunStatus{
		Phase:   runPhase,
		Message: message,
	}

	return executionDerivedStatus{
		Observed: model.ServiceObservedStatus{
			ObservedGeneration: item.GetServiceGeneration(),
			Phase:              phase,
			Message:            message,
			LastObservedAt:     &now,
		},
		Run: runStatus,
	}
}

func executionSnapshotMessage(item *cloudplanev1.PlaneExecutionSnapshot) string {
	if strings.TrimSpace(item.GetLastStatusReason()) != "" {
		return item.GetLastStatusReason()
	}
	switch strings.TrimSpace(item.GetStatus()) {
	case planeExecutionStatusFailed:
		return fmt.Sprintf("execution plan %s failed", item.GetPlanId())
	case planeExecutionStatusRunning:
		return fmt.Sprintf("execution plan %s is running", item.GetPlanId())
	case planeExecutionStatusSucceeded:
		return fmt.Sprintf("execution plan %s succeeded", item.GetPlanId())
	default:
		return fmt.Sprintf("execution plan %s is %s", item.GetPlanId(), strings.TrimSpace(item.GetStatus()))
	}
}

func derivePlaneStatus(planeDetail model.PlaneDetail, snapshot *cloudplanev1.PlaneSnapshot) (string, string) {
	overview := snapshotOverview(snapshot)
	alertsFiring := int(snapshot.GetReliability().GetAlertsFiring())

	issues := make([]string, 0, 3)
	if snapshot.GetPlane().GetProvider() != "" && snapshot.GetPlane().GetProvider() != planeDetail.Provider {
		issues = append(issues, fmt.Sprintf("provider mismatch: expected %s but remote reports %s", planeDetail.Provider, snapshot.GetPlane().GetProvider()))
	}
	if snapshot.GetPlane().GetRegion() != "" && snapshot.GetPlane().GetRegion() != planeDetail.Region {
		issues = append(issues, fmt.Sprintf("region mismatch: expected %s but remote reports %s", planeDetail.Region, snapshot.GetPlane().GetRegion()))
	}

	unhealthyNodes := overview.NodesOffline + overview.NodesDraining
	if unhealthyNodes > 0 {
		issues = append(issues, fmt.Sprintf("%d node(s) are not ready/offline/draining", unhealthyNodes))
	}
	if alertsFiring > 0 {
		issues = append(issues, fmt.Sprintf("%d reliability alert(s) are firing", alertsFiring))
	}

	if len(issues) == 0 {
		return model.StatusReady, fmt.Sprintf(
			"sync healthy: %d nodes, %d execution plans",
			overview.NodesTotal,
			overview.ExecutionPlansTotal,
		)
	}

	return model.StatusDegraded, "sync degraded: " + strings.Join(issues, "; ")
}

func buildNodeInventory(snapshot *cloudplanev1.PlaneSnapshot) store.RecordNodeInventoryInput {
	nodeInventory := snapshot.GetNodeInventory()
	out := store.RecordNodeInventoryInput{
		ObservedAt: protoTime(nodeInventory.GetObservedAt()),
		Nodes:      make([]model.PlaneNode, 0, len(nodeInventory.GetNodes())),
	}
	for _, item := range nodeInventory.GetNodes() {
		if item == nil {
			continue
		}
		if item.GetStatus() == "ready" {
			out.NodesReady++
		}
		out.NodesTotal++
		out.CPUMilliCapacity += int(item.GetCpuMilliAllocatable())
		out.CPUMilliAllocated += int(item.GetCpuMilliAllocated())
		out.MemoryMiCapacity += int(item.GetMemoryMiAllocatable())
		out.MemoryMiAllocated += int(item.GetMemoryMiAllocated())
		out.Nodes = append(out.Nodes, model.PlaneNode{
			NodeID:            item.GetNodeId(),
			Name:              item.GetName(),
			Provider:          item.GetProvider(),
			Region:            item.GetRegion(),
			InstanceID:        item.GetInstanceId(),
			InstanceType:      item.GetInstanceType(),
			Status:            item.GetStatus(),
			Schedulable:       item.GetSchedulable(),
			Elastic:           item.GetElastic(),
			CPUMilliCapacity:  int(item.GetCpuMilliAllocatable()),
			CPUMilliAllocated: int(item.GetCpuMilliAllocated()),
			MemoryMiCapacity:  int(item.GetMemoryMiAllocatable()),
			MemoryMiAllocated: int(item.GetMemoryMiAllocated()),
			LastHeartbeatAt:   protoTimePtr(item.GetLastHeartbeatAt()),
		})
	}
	return out
}

func snapshotOverview(snapshot *cloudplanev1.PlaneSnapshot) syncOverview {
	out := syncOverview{ExecutionPlansTotal: len(snapshot.GetExecutions())}
	for _, item := range snapshot.GetNodeInventory().GetNodes() {
		if item == nil {
			continue
		}
		out.NodesTotal++
		switch strings.TrimSpace(item.GetStatus()) {
		case "ready":
			out.NodesReady++
		case "draining":
			out.NodesDraining++
		case "offline":
			out.NodesOffline++
		}
	}
	return out
}

func protoTime(item *timestamppb.Timestamp) time.Time {
	if item == nil {
		return time.Time{}
	}
	return item.AsTime().UTC()
}

func protoTimePtr(item *timestamppb.Timestamp) *time.Time {
	if item == nil {
		return nil
	}
	value := item.AsTime().UTC()
	return &value
}
