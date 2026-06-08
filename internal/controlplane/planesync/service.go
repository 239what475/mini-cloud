package planesync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/contract/cloudplaneapi"
	domain "mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

var (
	ErrPlaneNotRegistered = errors.New("plane southbound token is not configured")
)

const (
	backgroundPlaneSyncPerPlaneTimeout = 30 * time.Second
	ManualPlaneSyncTimeout             = 2 * time.Minute
)

type Syncer struct {
	logger *slog.Logger
	store  *store.Store
	now    func() time.Time
}

type Result struct {
	Plane            domain.Detail                 `json:"plane"`
	ObservedProvider string                        `json:"observedProvider"`
	ObservedRegion   string                        `json:"observedRegion"`
	HealthCheckedAt  time.Time                     `json:"healthCheckedAt"`
	SyncedAt         time.Time                     `json:"syncedAt"`
	Overview         cloudplaneapi.OverviewSummary `json:"overview"`
	AlertsFiring     int                           `json:"alertsFiring"`
}

type PlaneSyncOutcome struct {
	PlaneID string  `json:"planeID"`
	Result  *Result `json:"result,omitempty"`
	Error   string  `json:"error,omitempty"`
}

type planeSnapshot struct {
	Plane         cloudplaneapi.PlaneSummary
	Health        cloudplaneapi.HealthSummary
	Overview      cloudplaneapi.OverviewSummary
	Capacity      cloudplaneapi.CapacitySummary
	Reliability   cloudplaneapi.ReliabilitySummary
	Runtime       cloudplaneapi.RuntimeInventory
	RuntimeConfig cloudplaneapi.RuntimeConfigSnapshot
	Executions    []cloudplaneapi.ExecutionSnapshot
}

type syncError struct {
	status          string
	message         string
	lastHeartbeatAt *time.Time
}

func (e *syncError) Error() string {
	return e.message
}

func IsSyncFailure(err error) bool {
	var target *syncError
	return errors.As(err, &target)
}

func NewSyncer(logger *slog.Logger, stores *store.Store) *Syncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{
		logger: logger,
		store:  stores,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Syncer) SyncPlane(ctx context.Context, planeID string) (Result, error) {
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		if errors.Is(err, store.ErrPlaneSouthboundTokenNotFound) {
			return Result{}, ErrPlaneNotRegistered
		}
		return Result{}, err
	}
	return s.syncWithToken(ctx, planeID, token)
}

func (s *Syncer) syncRegisteredPlanes(ctx context.Context, perPlaneTimeout time.Duration) ([]PlaneSyncOutcome, error) {
	planeIDs, err := s.store.ListRegisteredPlaneIDs(ctx)
	if err != nil {
		return nil, err
	}

	outcomes := make([]PlaneSyncOutcome, 0, len(planeIDs))
	for _, planeID := range planeIDs {
		planeCtx := ctx
		cancel := func() {}
		if perPlaneTimeout > 0 {
			planeCtx, cancel = context.WithTimeout(ctx, perPlaneTimeout)
		}
		result, err := s.SyncPlane(planeCtx, planeID)
		cancel()
		outcome := PlaneSyncOutcome{PlaneID: planeID}
		if err != nil {
			outcome.Error = err.Error()
		} else {
			outcome.Result = &result
		}
		outcomes = append(outcomes, outcome)
		if ctx.Err() != nil {
			return outcomes, ctx.Err()
		}
	}
	return outcomes, nil
}

func StartLoop(ctx context.Context, logger *slog.Logger, syncer *Syncer, interval int) {
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

func (s *Syncer) syncWithToken(ctx context.Context, planeID string, southboundToken string) (Result, error) {
	ctx = logctx.WithFields(ctx, logctx.Fields{PlaneID: planeID})
	logger := logctx.Logger(ctx, s.logger)
	planeDetail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return Result{}, err
	}

	grpcEndpoint := strings.TrimRight(strings.TrimSpace(planeDetail.GRPCEndpoint), "/")
	client, err := planeclient.New(grpcEndpoint, southboundToken)
	if err != nil {
		err = &syncError{
			status:  domain.StatusOffline,
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
		return Result{}, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			logger.Warn("close plane southbound client failed", "error", closeErr)
		}
	}()

	snapshotResp, err := client.Snapshot(ctx)
	if err != nil {
		syncErr := &syncError{
			status:  domain.StatusOffline,
			message: fmt.Sprintf("load plane snapshot failed: %v", err),
		}
		if updateErr := s.updateFailedPlaneStatus(ctx, planeID, syncErr); updateErr != nil {
			logger.Error("update failed plane status failed", "error", updateErr)
		}
		return Result{}, syncErr
	}
	snapshot := planeSnapshot{
		Plane:         snapshotResp.Plane,
		Health:        snapshotResp.Health,
		Overview:      snapshotResp.Overview,
		Capacity:      snapshotResp.Capacity,
		Reliability:   snapshotResp.Reliability,
		Runtime:       snapshotResp.Runtime,
		RuntimeConfig: snapshotResp.RuntimeConfig,
		Executions:    snapshotResp.Executions,
	}

	if _, err := s.store.MarkPlaneSouthboundTokenVerified(ctx, planeID, snapshot.Health.CheckedAt); err != nil {
		return Result{}, err
	}
	syncedAt := s.now()
	status, message, alertsFiring := derivePlaneStatus(planeDetail, snapshot)
	if _, err := s.store.UpdatePlaneStatus(ctx, planeID, domain.PlaneUpdateStatusInput{
		Status:          status,
		Message:         message,
		LastHeartbeatAt: &snapshot.Health.CheckedAt,
		LastSyncAt:      &syncedAt,
	}); err != nil {
		return Result{}, err
	}
	if _, _, err := s.store.ReplacePlaneRuntimeInventory(ctx, planeID, buildRuntimeInventory(snapshot)); err != nil {
		return Result{}, err
	}
	if _, err := s.store.RecordPlaneRuntimeConfig(ctx, planeID, buildRuntimeConfig(snapshot)); err != nil {
		return Result{}, err
	}
	if err := s.applyExecutionSnapshots(ctx, planeID, snapshot.Executions); err != nil {
		return Result{}, err
	}

	detail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Plane:            detail,
		ObservedProvider: snapshot.Plane.Provider,
		ObservedRegion:   snapshot.Plane.Region,
		HealthCheckedAt:  snapshot.Health.CheckedAt,
		SyncedAt:         syncedAt,
		Overview:         snapshot.Overview,
		AlertsFiring:     alertsFiring,
	}, nil
}

func (s *Syncer) applyExecutionSnapshots(ctx context.Context, planeID string, executions []cloudplaneapi.ExecutionSnapshot) error {
	for _, item := range executions {
		if strings.TrimSpace(item.ServiceID) == "" || item.ServiceGeneration <= 0 {
			continue
		}
		serviceItem, err := s.store.GetService(ctx, item.ServiceID)
		if err != nil {
			if errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
		if strings.TrimSpace(serviceItem.Status.Observed.AssignedPlaneID) != planeID {
			continue
		}
		if item.ServiceGeneration != serviceItem.Metadata.Generation {
			continue
		}
		status := serviceStatusFromExecutionSnapshot(serviceItem, item)
		if serviceItem.Status.DesiredState == domain.DesiredStateDeleted && deleteExecutionPlanComplete(item) {
			if err := s.store.DeleteServiceForGeneration(ctx, item.ServiceID, item.ServiceGeneration); err != nil &&
				!errors.Is(err, store.ErrServiceNotFound) &&
				!errors.Is(err, store.ErrServiceGenerationConflict) {
				return err
			}
			continue
		}
		if _, err := s.store.UpdateServiceStatusForGeneration(ctx, item.ServiceID, item.ServiceGeneration, domain.ServiceUpdateStatusInput{
			ObservedGeneration: status.ObservedGeneration,
			Phase:              status.Phase,
			Healthy:            status.Healthy,
			Message:            status.Message,
			LastReconciledAt:   status.LastReconciledAt,
			Run:                &status.Run,
			RemoteStatus:       &status.RemoteStatus,
			RemoteMessage:      &status.RemoteMessage,
		}); err != nil {
			if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

func deleteExecutionPlanComplete(item cloudplaneapi.ExecutionSnapshot) bool {
	return strings.TrimSpace(item.Status) == "superseded"
}

type executionDerivedStatus struct {
	domain.Status
	Run domain.RunStatus
}

func serviceStatusFromExecutionSnapshot(serviceItem domain.Service, item cloudplaneapi.ExecutionSnapshot) executionDerivedStatus {
	now := item.ObservedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	message := executionSnapshotMessage(item)
	phase := domain.PhaseProgressing
	healthy := false
	runPhase := domain.RunPhaseDispatching

	switch strings.TrimSpace(item.Status) {
	case "failed":
		phase = domain.PhaseDegraded
		runPhase = domain.RunPhaseFailed
	case "running":
		phase = domain.PhaseReady
		healthy = true
		runPhase = domain.RunPhaseRunning
	case "superseded":
		runPhase = domain.RunPhaseSuperseded
	}
	runStatus := domain.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = item.PlanID
	if runPhase == domain.RunPhaseRunning {
		runStatus.CurrentRunID = item.PlanID
	}
	runStatus.Phase = runPhase
	runStatus.Message = message
	runStatus.LastObservedAt = &now

	return executionDerivedStatus{
		Status: domain.Status{
			ObservedGeneration: item.ServiceGeneration,
			Phase:              phase,
			Healthy:            healthy,
			Message:            message,
			RemoteStatus:       strings.TrimSpace(item.Status),
			RemoteMessage:      message,
			LastReconciledAt:   &now,
		},
		Run: runStatus,
	}
}

func executionSnapshotMessage(item cloudplaneapi.ExecutionSnapshot) string {
	if strings.TrimSpace(item.LastStatusReason) != "" {
		return item.LastStatusReason
	}
	switch strings.TrimSpace(item.Status) {
	case "failed":
		return fmt.Sprintf("execution plan %s failed", item.PlanID)
	case "running":
		return fmt.Sprintf("execution plan %s is running", item.PlanID)
	case "superseded":
		return fmt.Sprintf("execution plan %s is superseded", item.PlanID)
	default:
		return fmt.Sprintf("execution plan %s is %s", item.PlanID, strings.TrimSpace(item.Status))
	}
}

func (s *Syncer) updateFailedPlaneStatus(ctx context.Context, planeID string, syncErr *syncError) error {
	syncedAt := s.now()
	_, err := s.store.UpdatePlaneStatus(ctx, planeID, domain.PlaneUpdateStatusInput{
		Status:          syncErr.status,
		Message:         syncErr.message,
		LastHeartbeatAt: syncErr.lastHeartbeatAt,
		LastSyncAt:      &syncedAt,
	})
	return err
}

func derivePlaneStatus(planeDetail domain.Detail, snapshot planeSnapshot) (string, string, int) {
	alertsFiring := snapshot.Reliability.AlertsFiring

	issues := make([]string, 0, 3)
	if snapshot.Health.Service != "" && snapshot.Health.Service != "ok" {
		issues = append(issues, fmt.Sprintf("remote plane service health is %s", snapshot.Health.Service))
	}
	if snapshot.Health.Database != "" && snapshot.Health.Database != "ok" {
		issues = append(issues, fmt.Sprintf("remote plane database health is %s", snapshot.Health.Database))
	}
	if !snapshot.Plane.Configured {
		issues = append(issues, "remote plane config is not fully configured")
	}
	if snapshot.Plane.Provider != "" && snapshot.Plane.Provider != planeDetail.Provider {
		issues = append(issues, fmt.Sprintf("provider mismatch: expected %s but remote reports %s", planeDetail.Provider, snapshot.Plane.Provider))
	}
	if snapshot.Plane.Region != "" && snapshot.Plane.Region != planeDetail.Region {
		issues = append(issues, fmt.Sprintf("region mismatch: expected %s but remote reports %s", planeDetail.Region, snapshot.Plane.Region))
	}

	unhealthyNodes := snapshot.Overview.NodesNotReady + snapshot.Overview.NodesOffline + snapshot.Overview.NodesDraining
	if unhealthyNodes > 0 {
		issues = append(issues, fmt.Sprintf("%d node(s) are not ready/offline/draining", unhealthyNodes))
	}
	if alertsFiring > 0 {
		issues = append(issues, fmt.Sprintf("%d reliability alert(s) are firing", alertsFiring))
	}

	if len(issues) == 0 {
		return domain.StatusReady, fmt.Sprintf(
			"sync healthy: %d nodes, %d services, %d execution plans",
			snapshot.Overview.NodesTotal,
			snapshot.Overview.ServicesTotal,
			snapshot.Overview.ExecutionPlansTotal,
		), alertsFiring
	}

	return domain.StatusDegraded, "sync degraded: " + strings.Join(issues, "; "), alertsFiring
}

func buildRuntimeInventory(snapshot planeSnapshot) domain.RecordRuntimeInventoryInput {
	out := domain.RecordRuntimeInventoryInput{
		SyncVersion:       snapshot.Runtime.SyncVersion,
		ObservedAt:        snapshot.Runtime.ObservedAt,
		NodesTotal:        snapshot.Capacity.RuntimeNodesTotal,
		NodesReady:        snapshot.Capacity.RuntimeNodesReady,
		CPUMilliCapacity:  snapshot.Capacity.CPUMilliAllocatable,
		CPUMilliAllocated: snapshot.Capacity.CPUMilliAllocated,
		MemoryMiCapacity:  snapshot.Capacity.MemoryMiAllocatable,
		MemoryMiAllocated: snapshot.Capacity.MemoryMiAllocated,
		Nodes:             make([]domain.RuntimeNode, 0, len(snapshot.Runtime.Nodes)),
	}
	for _, item := range snapshot.Runtime.Nodes {
		out.Nodes = append(out.Nodes, domain.RuntimeNode{
			NodeID:            item.NodeID,
			NodeEpoch:         item.NodeEpoch,
			Name:              item.Name,
			Provider:          item.Provider,
			Region:            item.Region,
			InstanceID:        item.InstanceID,
			InstanceType:      item.InstanceType,
			Status:            item.Status,
			Schedulable:       item.Schedulable,
			CPUMilliCapacity:  item.CPUMilliAllocatable,
			CPUMilliAllocated: item.CPUMilliAllocated,
			MemoryMiCapacity:  item.MemoryMiAllocatable,
			MemoryMiAllocated: item.MemoryMiAllocated,
			LastHeartbeatAt:   item.LastHeartbeatAt,
		})
	}
	return out
}

func buildRuntimeConfig(snapshot planeSnapshot) domain.RecordRuntimeConfigInput {
	return domain.RecordRuntimeConfigInput{
		ObservedAt:  snapshot.RuntimeConfig.ObservedAt,
		Fingerprint: snapshot.RuntimeConfig.Fingerprint,
		Summary:     snapshot.RuntimeConfig.Summary,
	}
}
