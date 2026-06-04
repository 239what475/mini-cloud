package servicecontroller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"mini-cloud/internal/contract/cloudplaneapi"
	"mini-cloud/internal/controlplane/deploy"
	plane "mini-cloud/internal/controlplane/plane"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/planeselector"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestCreateReconcilesServiceToPlacement(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create")

	planner := &fakePlanner{previewResult: planeselector.SelectionResult{Decision: &planeselector.Decision{PlaneID: planeItem.ID}}}
	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, planner, deployer)

	view, err := controller.Create(ctx, createInput("web", "Web", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if view.Service.Metadata.Generation != 1 {
		t.Fatalf("generation = %d, want 1", view.Service.Metadata.Generation)
	}
	if view.Service.Status.Observed.Phase != controlservice.PhaseReady {
		t.Fatalf("phase = %s, want ready", view.Service.Status.Observed.Phase)
	}
	if view.Placement == nil || view.Placement.PlaneID != planeItem.ID {
		t.Fatalf("placement = %+v, want plane %s", view.Placement, planeItem.ID)
	}
	if len(deployer.applyInputs) != 1 || deployer.applyInputs[0].Metadata.Name != "web" {
		t.Fatalf("unexpected apply inputs: %+v", deployer.applyInputs)
	}
}

func TestUpdateReusesCurrentPlacement(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update")

	planner := &fakePlanner{previewResult: planeselector.SelectionResult{Decision: &planeselector.Decision{PlaneID: planeItem.ID}}}
	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, planner, deployer)

	created, err := controller.Create(ctx, createInput("api", "API", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := controller.Update(ctx, created.Service.Metadata.ID, updateInput("API v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Placement == nil || updated.Placement.PlaneID != planeItem.ID {
		t.Fatalf("placement = %+v, want plane %s", updated.Placement, planeItem.ID)
	}
	if len(deployer.applyInputs) != 2 {
		t.Fatalf("applyInputs = %d, want 2", len(deployer.applyInputs))
	}
	if deployer.applyInputs[1].Spec.Image != "nginx:1.28-alpine" {
		t.Fatalf("updated image = %s, want nginx:1.28-alpine", deployer.applyInputs[1].Spec.Image)
	}
}

func TestDeleteRemovesServiceAndPlacement(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-delete")

	planner := &fakePlanner{previewResult: planeselector.SelectionResult{Decision: &planeselector.Decision{PlaneID: planeItem.ID}}}
	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, planner, deployer)

	created, err := controller.Create(ctx, createInput("gone", "Gone", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	serviceID := created.Service.Metadata.ID
	if _, err := controller.Delete(ctx, serviceID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := db.Store.GetService(ctx, serviceID); !errors.Is(err, store.ErrServiceNotFound) {
		t.Fatalf("GetService after delete error = %v, want ErrServiceNotFound", err)
	}
}

func createInput(name string, displayName string, image string) controlservice.CreateInput {
	return controlservice.CreateInput{Name: name, DisplayName: displayName, Spec: serviceSpec(image)}
}

func updateInput(displayName string, image string) controlservice.UpdateInput {
	return controlservice.UpdateInput{DisplayName: displayName, Spec: serviceSpec(image)}
}

func serviceSpec(image string) controlservice.Spec {
	return controlservice.Spec{
		Provider:       "aliyun",
		Region:         "cn-beijing",
		Replicas:       1,
		InstanceClass:  controlservice.InstanceClassSmall,
		Exposure:       "public",
		Image:          image,
		DefaultPort:    80,
		ReadinessPath:  "/",
		RevisionPolicy: controlservice.RevisionPolicy{}.Normalized(),
	}
}

type fakePlanner struct {
	previewCalls   int
	previewResults []planeselector.SelectionResult
	previewResult  planeselector.SelectionResult
	previewErr     error
	lastInput      planeselector.SelectionInput
}

func (f *fakePlanner) PreviewSelection(_ context.Context, input planeselector.SelectionInput) (planeselector.SelectionResult, error) {
	f.previewCalls++
	f.lastInput = input
	if f.previewErr != nil {
		return planeselector.SelectionResult{}, f.previewErr
	}
	if len(f.previewResults) > 0 {
		idx := f.previewCalls - 1
		if idx >= len(f.previewResults) {
			idx = len(f.previewResults) - 1
		}
		return f.previewResults[idx], nil
	}
	return f.previewResult, nil
}

type fakeDeploy struct {
	applyInputs []deploy.ApplyServiceInput
	remoteByKey map[string]cloudplaneapi.ServiceResponse
	deleteCalls int
}

func newFakeDeploy() *fakeDeploy {
	return &fakeDeploy{remoteByKey: make(map[string]cloudplaneapi.ServiceResponse)}
}

func (f *fakeDeploy) ApplyService(_ context.Context, planeID string, input deploy.ApplyServiceInput) (deploy.ApplyResult, error) {
	f.applyInputs = append(f.applyInputs, input)
	response := planeServiceResponse(input.Metadata.Name, cloudplaneapi.ObservedRolloutStatus{Phase: controlservice.RolloutPhaseIdle, StableRevisionID: "rev-1", StableDesiredReplicas: input.Spec.Replicas, StableReadyReplicas: input.Spec.Replicas, StableAvailableReplicas: input.Spec.Replicas})
	f.remoteByKey[planeServiceKey(planeID, input.Metadata.ID)] = response
	return deploy.ApplyResult{PlaneID: planeID, Action: deploy.ApplyActionUpdated, DesiredGeneration: int64(len(f.applyInputs))}, nil
}

func (f *fakeDeploy) DeleteService(_ context.Context, planeID string, serviceID string) error {
	f.deleteCalls++
	delete(f.remoteByKey, planeServiceKey(planeID, serviceID))
	return nil
}

func (f *fakeDeploy) GetService(_ context.Context, planeID string, serviceID string) (cloudplaneapi.ServiceResponse, error) {
	item, ok := f.remoteByKey[planeServiceKey(planeID, serviceID)]
	if !ok {
		return cloudplaneapi.ServiceResponse{}, planeclient.ErrNotFound
	}
	return item, nil
}

func planeServiceResponse(serviceID string, rollout cloudplaneapi.ObservedRolloutStatus) cloudplaneapi.ServiceResponse {
	currentRevisionID := rollout.StableRevisionID
	if strings.TrimSpace(currentRevisionID) == "" {
		currentRevisionID = rollout.CandidateRevisionID
	}
	return cloudplaneapi.ServiceResponse{
		Service: cloudplaneapi.Service{
			Metadata: cloudplaneapi.ServiceMetadata{ID: serviceID, Name: serviceID, DisplayName: serviceID},
			Spec:     cloudplaneapi.ServiceSpec{Region: "cn-beijing", Replicas: 1, InstanceClass: controlservice.InstanceClassSmall, Exposure: "public", Image: "nginx:1.27-alpine", DefaultPort: 80, ReadinessPath: "/"},
			Status:   cloudplaneapi.ServiceStatus{Phase: "running", CurrentRevisionID: currentRevisionID},
		},
		Status: cloudplaneapi.ObservedServiceStatus{Healthy: true, Message: "ready", CurrentRevisionID: currentRevisionID, Rollout: rollout},
	}
}

func planeServiceKey(planeID string, serviceID string) string { return planeID + "/" + serviceID }

func mustCreateReadyPlane(t *testing.T, db testutil.ControlPlaneTestDatabase, name string) plane.Detail {
	t.Helper()
	ctx := context.Background()
	item, err := db.Store.CreatePlane(ctx, plane.CreateInput{Name: name, DisplayName: name, Provider: "aliyun", Region: "cn-beijing", GRPCEndpoint: name + ".example.test:443"})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}
	if _, err := db.Store.SetPlaneSouthboundToken(ctx, item.ID, "southbound-"+name); err != nil {
		t.Fatalf("SetPlaneSouthboundToken returned error: %v", err)
	}
	if _, err := db.Store.UpdatePlaneStatus(ctx, item.ID, plane.UpdateStatusInput{Status: plane.StatusReady, Message: "ready"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	detail, err := db.Store.GetPlane(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	return detail
}

var _ = fmt.Sprintf
