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
	plane "mini-cloud/internal/controlplane/plane"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/store"
)

var (
	ErrPlaneNotRegistered = errors.New("plane southbound token is not configured")
)

const (
	backgroundPlaneSyncPerPlaneTimeout = 30 * time.Second
	ManualPlaneSyncTimeout             = 2 * time.Minute
)

type Service struct {
	logger  *slog.Logger
	store   serviceStore
	fetcher planeSnapshotFetcher
	now     func() time.Time
}

type Result struct {
	Plane            plane.Detail                  `json:"plane"`
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

type serviceStore interface {
	GetPlane(context.Context, string) (plane.Detail, error)
	SetPlaneSouthboundToken(context.Context, string, string) (plane.Registration, error)
	MarkPlaneSouthboundTokenVerified(context.Context, string, time.Time) (plane.Registration, error)
	GetPlaneSouthboundToken(context.Context, string) (string, error)
	ListRegisteredPlaneIDs(context.Context) ([]string, error)
	UpdatePlaneStatus(context.Context, string, plane.UpdateStatusInput) (plane.PlaneStatus, error)
	ReplacePlaneRuntimeInventory(context.Context, string, plane.RecordRuntimeInventoryInput) (plane.RuntimeInventorySnapshot, []plane.RuntimeNode, error)
	RecordPlaneRuntimeConfig(context.Context, string, plane.RecordRuntimeConfigInput) (plane.RuntimeConfigSnapshot, error)
	GetService(context.Context, string) (controlservice.Service, error)
	UpdateServiceStatusForGeneration(context.Context, string, int64, controlservice.UpdateStatusInput) (controlservice.Service, error)
	DeleteServiceForGeneration(context.Context, string, int64) error
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

type planeSnapshotFetcher interface {
	Fetch(context.Context, string, string) (planeSnapshot, error)
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

func NewService(logger *slog.Logger, stores *store.Store) *Service {
	return NewServiceWithFetcher(logger, stores, newGRPCPlaneSnapshotFetcher())
}

func NewServiceWithFetcher(logger *slog.Logger, stores serviceStore, fetcher planeSnapshotFetcher) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:  logger,
		store:   stores,
		fetcher: fetcher,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) RegisterPlane(ctx context.Context, planeID string, input plane.RegisterInput) (Result, error) {
	if err := input.Validate(); err != nil {
		return Result{}, err
	}
	return s.syncWithToken(ctx, planeID, strings.TrimSpace(input.SouthboundToken), true)
}

func (s *Service) SyncPlane(ctx context.Context, planeID string) (Result, error) {
	token, err := s.store.GetPlaneSouthboundToken(ctx, planeID)
	if err != nil {
		if errors.Is(err, store.ErrPlaneSouthboundTokenNotFound) {
			return Result{}, ErrPlaneNotRegistered
		}
		return Result{}, err
	}
	return s.syncWithToken(ctx, planeID, token, false)
}

func (s *Service) SyncRegisteredPlanes(ctx context.Context) ([]PlaneSyncOutcome, error) {
	return s.syncRegisteredPlanes(ctx, 0)
}

func (s *Service) syncRegisteredPlanes(ctx context.Context, perPlaneTimeout time.Duration) ([]PlaneSyncOutcome, error) {
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

func StartLoop(ctx context.Context, logger *slog.Logger, service *Service, interval time.Duration) {
	if service == nil || interval <= 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				outcomes, err := service.syncRegisteredPlanes(ctx, backgroundPlaneSyncPerPlaneTimeout)
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

func (s *Service) syncWithToken(ctx context.Context, planeID string, southboundToken string, persistToken bool) (Result, error) {
	ctx = logctx.WithFields(ctx, logctx.Fields{PlaneID: planeID})
	logger := logctx.Logger(ctx, s.logger)
	planeDetail, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		return Result{}, err
	}

	snapshot, err := s.fetcher.Fetch(ctx, planeDetail.GRPCEndpoint, southboundToken)
	if err != nil {
		var syncErr *syncError
		if errors.As(err, &syncErr) {
			if updateErr := s.updateFailedPlaneStatus(ctx, planeID, syncErr); updateErr != nil {
				logger.Error("update failed plane status failed", "error", updateErr)
			}
		}
		return Result{}, err
	}

	if persistToken {
		if _, err := s.store.SetPlaneSouthboundToken(ctx, planeID, southboundToken); err != nil {
			return Result{}, err
		}
	}
	if _, err := s.store.MarkPlaneSouthboundTokenVerified(ctx, planeID, snapshot.Health.CheckedAt); err != nil {
		return Result{}, err
	}
	syncedAt := s.now()
	status, message, alertsFiring := derivePlaneStatus(planeDetail, snapshot)
	if _, err := s.store.UpdatePlaneStatus(ctx, planeID, plane.UpdateStatusInput{
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

func (s *Service) applyExecutionSnapshots(ctx context.Context, planeID string, executions []cloudplaneapi.ExecutionSnapshot) error {
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
		if serviceItem.Status.DesiredState == controlservice.DesiredStateDeleted && deleteExecutionPlanComplete(item) {
			if err := s.store.DeleteServiceForGeneration(ctx, item.ServiceID, item.ServiceGeneration); err != nil &&
				!errors.Is(err, store.ErrServiceNotFound) &&
				!errors.Is(err, store.ErrServiceGenerationConflict) {
				return err
			}
			continue
		}
		if _, err := s.store.UpdateServiceStatusForGeneration(ctx, item.ServiceID, item.ServiceGeneration, controlservice.UpdateStatusInput{
			ObservedGeneration: status.ObservedGeneration,
			Phase:              status.Phase,
			Healthy:            status.Healthy,
			Message:            status.Message,
			Conditions:         status.Conditions,
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
	controlservice.Status
	Run controlservice.RunStatus
}

func serviceStatusFromExecutionSnapshot(serviceItem controlservice.Service, item cloudplaneapi.ExecutionSnapshot) executionDerivedStatus {
	now := item.ObservedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	message := executionSnapshotMessage(item)
	phase := controlservice.PhaseProgressing
	healthy := false
	readyCondition := controlservice.ConditionFalse
	readyReason := controlservice.ReasonPlaneServiceNotHealthy
	runPhase := controlservice.RunPhaseDispatching

	switch strings.TrimSpace(item.Status) {
	case "failed":
		phase = controlservice.PhaseDegraded
		runPhase = controlservice.RunPhaseFailed
	case "running":
		phase = controlservice.PhaseReady
		healthy = true
		readyCondition = controlservice.ConditionTrue
		readyReason = controlservice.ReasonPlaneServiceReady
		runPhase = controlservice.RunPhaseRunning
	case "superseded":
		runPhase = controlservice.RunPhaseSuperseded
	}
	runStatus := controlservice.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = item.PlanID
	if runPhase == controlservice.RunPhaseRunning {
		runStatus.CurrentRunID = item.PlanID
	}
	runStatus.Phase = runPhase
	runStatus.Message = message
	runStatus.LastObservedAt = &now

	return executionDerivedStatus{
		Status: controlservice.Status{
			ObservedGeneration: item.ServiceGeneration,
			Phase:              phase,
			Healthy:            healthy,
			Message:            message,
			RemoteStatus:       strings.TrimSpace(item.Status),
			RemoteMessage:      message,
			Conditions: []controlservice.Condition{
				controlservice.NewCondition(controlservice.ConditionAssignmentReady, controlservice.ConditionTrue, controlservice.ReasonApplied, "service assignment accepted by cloud-plane", item.ServiceGeneration, now),
				controlservice.NewCondition(controlservice.ConditionApplied, controlservice.ConditionTrue, controlservice.ReasonApplied, "execution plan accepted by cloud-plane", item.ServiceGeneration, now),
				controlservice.NewCondition(controlservice.ConditionReady, readyCondition, readyReason, message, item.ServiceGeneration, now),
			},
			LastReconciledAt: &now,
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

func (s *Service) updateFailedPlaneStatus(ctx context.Context, planeID string, syncErr *syncError) error {
	syncedAt := s.now()
	_, err := s.store.UpdatePlaneStatus(ctx, planeID, plane.UpdateStatusInput{
		Status:          syncErr.status,
		Message:         syncErr.message,
		LastHeartbeatAt: syncErr.lastHeartbeatAt,
		LastSyncAt:      &syncedAt,
	})
	return err
}

func derivePlaneStatus(planeDetail plane.Detail, snapshot planeSnapshot) (string, string, int) {
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
		return plane.StatusReady, fmt.Sprintf(
			"sync healthy: %d nodes, %d services, %d execution plans",
			snapshot.Overview.NodesTotal,
			snapshot.Overview.ServicesTotal,
			snapshot.Overview.ExecutionPlansTotal,
		), alertsFiring
	}

	return plane.StatusDegraded, "sync degraded: " + strings.Join(issues, "; "), alertsFiring
}

func buildRuntimeInventory(snapshot planeSnapshot) plane.RecordRuntimeInventoryInput {
	out := plane.RecordRuntimeInventoryInput{
		SyncVersion:       snapshot.Runtime.SyncVersion,
		ObservedAt:        snapshot.Runtime.ObservedAt,
		NodesTotal:        snapshot.Capacity.RuntimeNodesTotal,
		NodesReady:        snapshot.Capacity.RuntimeNodesReady,
		CPUMilliCapacity:  snapshot.Capacity.CPUMilliAllocatable,
		CPUMilliAllocated: snapshot.Capacity.CPUMilliAllocated,
		MemoryMiCapacity:  snapshot.Capacity.MemoryMiAllocatable,
		MemoryMiAllocated: snapshot.Capacity.MemoryMiAllocated,
		Nodes:             make([]plane.RuntimeNode, 0, len(snapshot.Runtime.Nodes)),
	}
	for _, item := range snapshot.Runtime.Nodes {
		out.Nodes = append(out.Nodes, plane.RuntimeNode{
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

func buildRuntimeConfig(snapshot planeSnapshot) plane.RecordRuntimeConfigInput {
	return plane.RecordRuntimeConfigInput{
		ObservedAt:  snapshot.RuntimeConfig.ObservedAt,
		Fingerprint: snapshot.RuntimeConfig.Fingerprint,
		Summary:     snapshot.RuntimeConfig.Summary,
	}
}
