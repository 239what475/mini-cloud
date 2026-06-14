package coordination

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
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
	planes          *PlaneCatalog
	southboundToken string
	southboundTLS   PlaneClientTLS
	dns             dnsClient
	now             func() time.Time
}

type syncOverview struct {
	NodesTotal      int
	NodesReady      int
	NodesDraining   int
	NodesOffline    int
	ActiveRunsTotal int
}

type PlaneSnapshotView struct {
	Plane    model.PlaneDetail
	Snapshot *cloudplanev1.PlaneSnapshot
}

func NewPlaneSyncer(logger *slog.Logger, planes *PlaneCatalog, southboundToken string, southboundTLS PlaneClientTLS, dns dnsClient) *PlaneSyncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaneSyncer{
		logger:          logger,
		planes:          planes,
		southboundToken: strings.TrimSpace(southboundToken),
		southboundTLS:   southboundTLS,
		dns:             dns,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *PlaneSyncer) CheckDNS(ctx context.Context) error {
	if s.dns == nil {
		return nil
	}
	return s.dns.Check(ctx)
}

func (s *PlaneSyncer) ListPlaneSnapshotViews(ctx context.Context, perPlaneTimeout time.Duration) ([]PlaneSnapshotView, error) {
	planes, err := s.planes.ListPlanes(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]PlaneSnapshotView, 0, len(planes))
	for _, plane := range planes {
		view := PlaneSnapshotView{Plane: plane}
		planeCtx := ctx
		cancel := func() {}
		if perPlaneTimeout > 0 {
			planeCtx, cancel = context.WithTimeout(ctx, perPlaneTimeout)
		}
		snapshot, err := s.loadSnapshot(planeCtx, plane)
		if err != nil {
			cancel()
			view.Plane = s.offlinePlane(plane, err)
			s.logger.Warn("load plane snapshot view failed", "plane_id", plane.ID, "error", err)
		} else {
			view.Snapshot = snapshot
			if err := s.syncFrontDoorDNS(planeCtx, plane.ID, snapshot.GetServices()); err != nil {
				s.logger.Warn("sync frontdoor DNS from plane snapshot failed", "plane_id", plane.ID, "error", err)
			}
			cancel()
			view.Plane = s.syncedPlane(plane, snapshot)
		}
		views = append(views, view)
		if ctx.Err() != nil {
			return views, ctx.Err()
		}
	}
	return views, nil
}

func (s *PlaneSyncer) GetPlaneSnapshotView(ctx context.Context, planeID string) (PlaneSnapshotView, error) {
	plane, err := s.planes.GetPlane(ctx, planeID)
	if err != nil {
		return PlaneSnapshotView{}, err
	}
	snapshot, err := s.loadSnapshot(ctx, plane)
	if err != nil {
		return PlaneSnapshotView{Plane: s.offlinePlane(plane, err)}, nil
	}
	if err := s.syncFrontDoorDNS(ctx, plane.ID, snapshot.GetServices()); err != nil {
		s.logger.Warn("sync frontdoor DNS from plane snapshot failed", "plane_id", plane.ID, "error", err)
	}
	return PlaneSnapshotView{Plane: s.syncedPlane(plane, snapshot), Snapshot: snapshot}, nil
}

func (s *PlaneSyncer) offlinePlane(plane model.PlaneDetail, err error) model.PlaneDetail {
	now := s.now()
	plane.Status = model.PlaneStatus{
		PlaneID:    plane.ID,
		Status:     model.StatusOffline,
		Message:    fmt.Sprintf("load plane snapshot failed: %v", err),
		LastSyncAt: &now,
	}
	return plane
}

func (s *PlaneSyncer) syncedPlane(plane model.PlaneDetail, snapshot *cloudplanev1.PlaneSnapshot) model.PlaneDetail {
	checkedAt := protoTime(snapshot.GetCheckedAt())
	syncedAt := s.now()
	status, message := derivePlaneStatus(plane, snapshot)
	plane.Status = model.PlaneStatus{
		PlaneID:         plane.ID,
		Status:          status,
		Message:         message,
		LastHeartbeatAt: &checkedAt,
		LastSyncAt:      &syncedAt,
	}
	return plane
}

func (s *PlaneSyncer) loadSnapshot(ctx context.Context, planeDetail model.PlaneDetail) (*cloudplanev1.PlaneSnapshot, error) {
	grpcEndpoint := strings.TrimRight(strings.TrimSpace(planeDetail.GRPCEndpoint), "/")
	client, err := newPlaneClient(grpcEndpoint, s.southboundToken, s.southboundTLS)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Warn("close plane southbound client failed", "plane_id", planeDetail.ID, "error", closeErr)
		}
	}()
	return client.Snapshot(ctx)
}

type executionDerivedStatus struct {
	Observed model.ServiceObservedStatus
	Run      model.RunStatus
}

func serviceStatusFromExecutionSnapshot(item *cloudplanev1.PlaneExecutionSnapshot) executionDerivedStatus {
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
		return "service run failed"
	case planeExecutionStatusRunning:
		return "service run is running"
	case planeExecutionStatusSucceeded:
		return "service cleanup finished"
	default:
		status := strings.TrimSpace(item.GetStatus())
		if status == "" {
			return "service run status is unknown"
		}
		return fmt.Sprintf("service run is %s", status)
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
			"sync healthy: %d nodes, %d active runs",
			overview.NodesTotal,
			overview.ActiveRunsTotal,
		)
	}

	return model.StatusDegraded, "sync degraded: " + strings.Join(issues, "; ")
}

func snapshotOverview(snapshot *cloudplanev1.PlaneSnapshot) syncOverview {
	out := syncOverview{ActiveRunsTotal: len(snapshot.GetExecutions())}
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
