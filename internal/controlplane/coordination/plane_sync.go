package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	errPlaneSyncNotRegistered = errors.New("plane southbound token is not configured")
)

const (
	backgroundPlaneSyncPerPlaneTimeout = 30 * time.Second
)

type PlaneSyncer struct {
	logger *slog.Logger
	store  *store.Store
	now    func() time.Time
}

type planeSyncResult struct {
	Plane            model.PlaneDetail `json:"plane"`
	ObservedProvider string            `json:"observedProvider"`
	ObservedRegion   string            `json:"observedRegion"`
	HealthCheckedAt  time.Time         `json:"healthCheckedAt"`
	SyncedAt         time.Time         `json:"syncedAt"`
	Overview         syncOverview      `json:"overview"`
	AlertsFiring     int               `json:"alertsFiring"`
}

type planeSyncOutcome struct {
	PlaneID         string           `json:"planeID"`
	planeSyncResult *planeSyncResult `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
}

type syncOverview struct {
	NodesTotal          int
	NodesReady          int
	NodesNotReady       int
	NodesDraining       int
	NodesOffline        int
	ExecutionPlansTotal int
}

type syncError struct {
	status          string
	message         string
	lastHeartbeatAt *time.Time
}

func (e *syncError) Error() string {
	return e.message
}

func NewPlaneSyncer(logger *slog.Logger, stores *store.Store) *PlaneSyncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaneSyncer{
		logger: logger,
		store:  stores,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *PlaneSyncer) syncPlane(ctx context.Context, planeID string) (planeSyncResult, error) {
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		if errors.Is(err, store.ErrPlaneSouthboundTokenNotFound) {
			return planeSyncResult{}, errPlaneSyncNotRegistered
		}
		return planeSyncResult{}, err
	}
	ctx = logctx.WithFields(ctx, logctx.Fields{PlaneID: planeID})
	logger := logctx.Logger(ctx, s.logger)
	planeDetail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return planeSyncResult{}, err
	}

	grpcEndpoint := strings.TrimRight(strings.TrimSpace(planeDetail.GRPCEndpoint), "/")
	client, err := newPlaneClient(grpcEndpoint, token)
	if err != nil {
		err = &syncError{
			status:  model.StatusOffline,
			message: fmt.Sprintf("initialize plane southbound client failed: %v", err),
		}
	}
	if err != nil {
		var syncErr *syncError
		if errors.As(err, &syncErr) {
			if updateErr := s.updateFailedPlaneStatus(ctx, planeID, syncErr); updateErr != nil {
				logger.Error("update failed plane status failed", "error", updateErr)
			}
		}
		return planeSyncResult{}, err
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
		if updateErr := s.updateFailedPlaneStatus(ctx, planeID, syncErr); updateErr != nil {
			logger.Error("update failed plane status failed", "error", updateErr)
		}
		return planeSyncResult{}, syncErr
	}
	healthCheckedAt := protoTime(snapshot.GetHealth().GetCheckedAt())

	if err := s.store.MarkPlaneSouthboundTokenVerified(ctx, planeID, healthCheckedAt); err != nil {
		return planeSyncResult{}, err
	}
	syncedAt := s.now()
	status, message, alertsFiring := derivePlaneStatus(planeDetail, snapshot)
	if err := s.store.UpdatePlaneStatus(ctx, planeID, store.UpdatePlaneStatusInput{
		Status:          status,
		Message:         message,
		LastHeartbeatAt: &healthCheckedAt,
		LastSyncAt:      &syncedAt,
	}); err != nil {
		return planeSyncResult{}, err
	}
	if err := s.store.ReplacePlaneRuntimeInventory(ctx, planeID, buildRuntimeInventory(snapshot)); err != nil {
		return planeSyncResult{}, err
	}
	if err := s.store.RecordPlaneRuntimeConfig(ctx, planeID, buildRuntimeConfig(snapshot)); err != nil {
		return planeSyncResult{}, err
	}
	if err := s.applyExecutionSnapshots(ctx, planeID, snapshot.GetExecutions()); err != nil {
		return planeSyncResult{}, err
	}

	detail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return planeSyncResult{}, err
	}

	return planeSyncResult{
		Plane:            detail,
		ObservedProvider: snapshot.GetPlane().GetProvider(),
		ObservedRegion:   snapshot.GetPlane().GetRegion(),
		HealthCheckedAt:  healthCheckedAt,
		SyncedAt:         syncedAt,
		Overview:         snapshotOverview(snapshot),
		AlertsFiring:     alertsFiring,
	}, nil
}

func (s *PlaneSyncer) syncRegisteredPlanes(ctx context.Context, perPlaneTimeout time.Duration) ([]planeSyncOutcome, error) {
	planeIDs, err := s.store.ListRegisteredPlaneIDs(ctx)
	if err != nil {
		return nil, err
	}

	outcomes := make([]planeSyncOutcome, 0, len(planeIDs))
	for _, planeID := range planeIDs {
		planeCtx := ctx
		cancel := func() {}
		if perPlaneTimeout > 0 {
			planeCtx, cancel = context.WithTimeout(ctx, perPlaneTimeout)
		}
		result, err := s.syncPlane(planeCtx, planeID)
		cancel()
		outcome := planeSyncOutcome{PlaneID: planeID}
		if err != nil {
			outcome.Error = err.Error()
		} else {
			outcome.planeSyncResult = &result
		}
		outcomes = append(outcomes, outcome)
		if ctx.Err() != nil {
			return outcomes, ctx.Err()
		}
	}
	return outcomes, nil
}

func StartPlaneSyncLoop(ctx context.Context, logger *slog.Logger, syncer *PlaneSyncer, interval int) {
	if syncer == nil || interval <= 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				outcomes, err := syncer.syncRegisteredPlanes(ctx, backgroundPlaneSyncPerPlaneTimeout)
				if err != nil {
					logger.Error("plane background sync failed", "error", err)
					continue
				}
				for _, outcome := range outcomes {
					if outcome.Error != "" {
						logger.Warn("plane sync failed", "plane_id", outcome.PlaneID, "error", outcome.Error)
					}
				}
			}
		}
	}()
}

func (s *PlaneSyncer) applyExecutionSnapshots(ctx context.Context, planeID string, executions []*cloudplanev1.PlaneExecutionSnapshot) error {
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
		if strings.TrimSpace(serviceItem.Status.Observed.AssignedPlaneID) != planeID {
			continue
		}
		if item.GetServiceGeneration() != serviceItem.Metadata.Generation {
			continue
		}
		status := serviceStatusFromExecutionSnapshot(serviceItem, item)
		if serviceItem.Status.DesiredState == model.DesiredStateDeleted && deleteExecutionPlanComplete(item) {
			if err := s.store.DeleteServiceForGeneration(ctx, item.GetServiceId(), item.GetServiceGeneration()); err != nil &&
				!errors.Is(err, store.ErrServiceNotFound) &&
				!errors.Is(err, store.ErrServiceGenerationConflict) {
				return err
			}
			continue
		}
		if err := s.store.UpdateServiceStatusForGeneration(ctx, item.GetServiceId(), item.GetServiceGeneration(), store.UpdateServiceStatusInput{
			ObservedGeneration: status.Observed.ObservedGeneration,
			Phase:              status.Observed.Phase,
			Healthy:            status.Observed.Healthy,
			Message:            status.Observed.Message,
			LastReconciledAt:   status.Observed.LastReconciledAt,
			Run:                &status.Run,
			RemoteStatus:       &status.Observed.RemoteStatus,
			RemoteMessage:      &status.Observed.RemoteMessage,
		}); err != nil {
			if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

func deleteExecutionPlanComplete(item *cloudplanev1.PlaneExecutionSnapshot) bool {
	return strings.TrimSpace(item.GetStatus()) == "superseded"
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
	healthy := false
	runPhase := model.RunPhaseDispatching

	switch strings.TrimSpace(item.GetStatus()) {
	case "failed":
		phase = model.PhaseDegraded
		runPhase = model.RunPhaseFailed
	case "running":
		phase = model.PhaseReady
		healthy = true
		runPhase = model.RunPhaseRunning
	case "superseded":
		runPhase = model.RunPhaseSuperseded
	}
	runStatus := model.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = item.GetPlanId()
	if runPhase == model.RunPhaseRunning {
		runStatus.CurrentRunID = item.GetPlanId()
	}
	runStatus.Phase = runPhase
	runStatus.Message = message
	runStatus.LastObservedAt = &now

	return executionDerivedStatus{
		Observed: model.ServiceObservedStatus{
			ObservedGeneration: item.GetServiceGeneration(),
			Phase:              phase,
			Healthy:            healthy,
			Message:            message,
			RemoteStatus:       strings.TrimSpace(item.GetStatus()),
			RemoteMessage:      message,
			LastReconciledAt:   &now,
		},
		Run: runStatus,
	}
}

func executionSnapshotMessage(item *cloudplanev1.PlaneExecutionSnapshot) string {
	if strings.TrimSpace(item.GetLastStatusReason()) != "" {
		return item.GetLastStatusReason()
	}
	switch strings.TrimSpace(item.GetStatus()) {
	case "failed":
		return fmt.Sprintf("execution plan %s failed", item.GetPlanId())
	case "running":
		return fmt.Sprintf("execution plan %s is running", item.GetPlanId())
	case "superseded":
		return fmt.Sprintf("execution plan %s is superseded", item.GetPlanId())
	default:
		return fmt.Sprintf("execution plan %s is %s", item.GetPlanId(), strings.TrimSpace(item.GetStatus()))
	}
}

func (s *PlaneSyncer) updateFailedPlaneStatus(ctx context.Context, planeID string, syncErr *syncError) error {
	syncedAt := s.now()
	err := s.store.UpdatePlaneStatus(ctx, planeID, store.UpdatePlaneStatusInput{
		Status:          syncErr.status,
		Message:         syncErr.message,
		LastHeartbeatAt: syncErr.lastHeartbeatAt,
		LastSyncAt:      &syncedAt,
	})
	return err
}

func derivePlaneStatus(planeDetail model.PlaneDetail, snapshot *cloudplanev1.PlaneSnapshot) (string, string, int) {
	overview := snapshotOverview(snapshot)
	alertsFiring := int(snapshot.GetReliability().GetAlertsFiring())

	issues := make([]string, 0, 3)
	if snapshot.GetHealth().GetService() != "" && snapshot.GetHealth().GetService() != "ok" {
		issues = append(issues, fmt.Sprintf("remote plane service health is %s", snapshot.GetHealth().GetService()))
	}
	if snapshot.GetHealth().GetDatabase() != "" && snapshot.GetHealth().GetDatabase() != "ok" {
		issues = append(issues, fmt.Sprintf("remote plane database health is %s", snapshot.GetHealth().GetDatabase()))
	}
	if !snapshot.GetPlane().GetConfigured() {
		issues = append(issues, "remote plane config is not fully configured")
	}
	if snapshot.GetPlane().GetProvider() != "" && snapshot.GetPlane().GetProvider() != planeDetail.Provider {
		issues = append(issues, fmt.Sprintf("provider mismatch: expected %s but remote reports %s", planeDetail.Provider, snapshot.GetPlane().GetProvider()))
	}
	if snapshot.GetPlane().GetRegion() != "" && snapshot.GetPlane().GetRegion() != planeDetail.Region {
		issues = append(issues, fmt.Sprintf("region mismatch: expected %s but remote reports %s", planeDetail.Region, snapshot.GetPlane().GetRegion()))
	}

	unhealthyNodes := overview.NodesNotReady + overview.NodesOffline + overview.NodesDraining
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
		), alertsFiring
	}

	return model.StatusDegraded, "sync degraded: " + strings.Join(issues, "; "), alertsFiring
}

func buildRuntimeInventory(snapshot *cloudplanev1.PlaneSnapshot) store.RecordRuntimeInventoryInput {
	runtimeInventory := snapshot.GetRuntimeInventory()
	out := store.RecordRuntimeInventoryInput{
		SyncVersion: runtimeInventory.GetSyncVersion(),
		ObservedAt:  protoTime(runtimeInventory.GetObservedAt()),
		Nodes:       make([]model.RuntimeNode, 0, len(runtimeInventory.GetNodes())),
	}
	for _, item := range runtimeInventory.GetNodes() {
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
		out.Nodes = append(out.Nodes, model.RuntimeNode{
			NodeID:            item.GetNodeId(),
			NodeEpoch:         item.GetNodeEpoch(),
			Name:              item.GetName(),
			Provider:          item.GetProvider(),
			Region:            item.GetRegion(),
			InstanceID:        item.GetInstanceId(),
			InstanceType:      item.GetInstanceType(),
			Status:            item.GetStatus(),
			Schedulable:       item.GetSchedulable(),
			CPUMilliCapacity:  int(item.GetCpuMilliAllocatable()),
			CPUMilliAllocated: int(item.GetCpuMilliAllocated()),
			MemoryMiCapacity:  int(item.GetMemoryMiAllocatable()),
			MemoryMiAllocated: int(item.GetMemoryMiAllocated()),
			LastHeartbeatAt:   protoTimePtr(item.GetLastHeartbeatAt()),
		})
	}
	return out
}

func buildRuntimeConfig(snapshot *cloudplanev1.PlaneSnapshot) store.RecordRuntimeConfigInput {
	runtimeConfig := snapshot.GetRuntimeConfig()
	summary := map[string]any{}
	if runtimeConfig.GetSummary() != nil {
		summary = runtimeConfig.GetSummary().AsMap()
	}
	return store.RecordRuntimeConfigInput{
		ObservedAt:  protoTime(runtimeConfig.GetObservedAt()),
		Fingerprint: runtimeConfig.GetFingerprint(),
		Summary:     summary,
	}
}

func snapshotOverview(snapshot *cloudplanev1.PlaneSnapshot) syncOverview {
	out := syncOverview{ExecutionPlansTotal: len(snapshot.GetExecutions())}
	for _, item := range snapshot.GetRuntimeInventory().GetNodes() {
		if item == nil {
			continue
		}
		out.NodesTotal++
		switch strings.TrimSpace(item.GetStatus()) {
		case "ready":
			out.NodesReady++
		case "not_ready":
			out.NodesNotReady++
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
