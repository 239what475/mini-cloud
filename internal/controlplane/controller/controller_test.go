package controller

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"mini-cloud/internal/controlplane/deploy"
	plane "mini-cloud/internal/controlplane/plane"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/testutil"
)

func TestCreateReconcilesServiceToAssignment(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create")

	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, deployer)

	view, err := controller.Create(ctx, createInput(planeItem.ID, "web", "Web", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if view.Service.Metadata.Generation != 1 {
		t.Fatalf("generation = %d, want 1", view.Service.Metadata.Generation)
	}
	if view.Service.Status.Observed.Phase != controlservice.PhaseReady {
		t.Fatalf("phase = %s, want ready", view.Service.Status.Observed.Phase)
	}
	if view.Service.Status.Observed.AssignedPlaneID != planeItem.ID {
		t.Fatalf("assigned plane = %q, want %s", view.Service.Status.Observed.AssignedPlaneID, planeItem.ID)
	}
	if len(deployer.applyInputs) != 1 || deployer.applyInputs[0].Metadata.Name != "web" {
		t.Fatalf("unexpected apply inputs: %+v", deployer.applyInputs)
	}
}

func TestUpdateReusesCurrentAssignment(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update")

	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, deployer)

	created, err := controller.Create(ctx, createInput(planeItem.ID, "api", "API", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := controller.Update(ctx, created.Service.Metadata.ID, updateInput(planeItem.ID, "API v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Service.Status.Observed.AssignedPlaneID != planeItem.ID {
		t.Fatalf("assigned plane = %q, want %s", updated.Service.Status.Observed.AssignedPlaneID, planeItem.ID)
	}
	if len(deployer.applyInputs) != 2 {
		t.Fatalf("applyInputs = %d, want 2", len(deployer.applyInputs))
	}
	if deployer.applyInputs[1].Spec.Image != "nginx:1.28-alpine" {
		t.Fatalf("updated image = %s, want nginx:1.28-alpine", deployer.applyInputs[1].Spec.Image)
	}
}

func TestUpdateMovesAssignmentWhenPlaneIDChanges(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeA := mustCreateReadyPlane(t, db, "plane-move-a")
	planeB := mustCreateReadyPlane(t, db, "plane-move-b")

	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, deployer)

	created, err := controller.Create(ctx, createInput(planeA.ID, "move", "Move", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := controller.Update(ctx, created.Service.Metadata.ID, updateInput(planeB.ID, "Move", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Service.Status.Observed.AssignedPlaneID != planeB.ID {
		t.Fatalf("assigned plane = %q, want %s", updated.Service.Status.Observed.AssignedPlaneID, planeB.ID)
	}
	if len(deployer.applyPlaneIDs) != 2 || deployer.applyPlaneIDs[0] != planeA.ID || deployer.applyPlaneIDs[1] != planeB.ID {
		t.Fatalf("apply plane ids = %+v, want [%s %s]", deployer.applyPlaneIDs, planeA.ID, planeB.ID)
	}
	if len(deployer.deletePlaneIDs) != 1 || deployer.deletePlaneIDs[0] != planeA.ID {
		t.Fatalf("delete plane ids = %+v, want [%s]", deployer.deletePlaneIDs, planeA.ID)
	}
}

func TestDeleteDispatchesDeletePlanAndKeepsServiceUntilPlaneSync(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-delete")

	deployer := newFakeDeploy()
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, deployer)

	created, err := controller.Create(ctx, createInput(planeItem.ID, "gone", "Gone", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	serviceID := created.Service.Metadata.ID
	if _, err := controller.Delete(ctx, serviceID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceID)
	if err != nil {
		t.Fatalf("GetService after delete returned error: %v", err)
	}
	if reloaded.Status.DesiredState != controlservice.DesiredStateDeleted || reloaded.Status.Observed.Phase != controlservice.PhaseDeleting {
		t.Fatalf("service status after delete = %+v, want deleting", reloaded.Status)
	}
	if len(deployer.deleteInputs) != 1 {
		t.Fatalf("deleteInputs len = %d, want 1", len(deployer.deleteInputs))
	}
	if deployer.deleteInputs[0].ServiceID != serviceID ||
		deployer.deleteInputs[0].ServiceGeneration != reloaded.Metadata.Generation ||
		deployer.deleteInputs[0].PlanID != serviceID+"-delete-g2" {
		t.Fatalf("delete input = %+v, want service generation delete plan", deployer.deleteInputs[0])
	}
}

func createInput(planeID string, name string, displayName string, image string) controlservice.CreateInput {
	return controlservice.CreateInput{Name: name, DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func updateInput(planeID string, displayName string, image string) controlservice.UpdateInput {
	return controlservice.UpdateInput{DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func serviceSpec(planeID string, image string) controlservice.Spec {
	return controlservice.Spec{
		PlaneID:       planeID,
		InstanceClass: controlservice.InstanceClassSmall,
		Exposure:      "public",
		Image:         image,
		DefaultPort:   80,
		ReadinessPath: "/",
	}
}

type fakeDeploy struct {
	applyPlaneIDs  []string
	applyInputs    []deploy.ApplyServiceInput
	deletePlaneIDs []string
	deleteInputs   []deploy.DeleteServiceInput
	deleteCalls    int
}

func newFakeDeploy() *fakeDeploy {
	return &fakeDeploy{}
}

func (f *fakeDeploy) ApplyService(_ context.Context, planeID string, input deploy.ApplyServiceInput) (deploy.ApplyResult, error) {
	f.applyPlaneIDs = append(f.applyPlaneIDs, planeID)
	f.applyInputs = append(f.applyInputs, input)
	return deploy.ApplyResult{PlaneID: planeID, Action: "updated", PlanID: fmt.Sprintf("%s-g%d", input.Metadata.ID, input.Metadata.Generation)}, nil
}

func (f *fakeDeploy) DeleteService(_ context.Context, planeID string, input deploy.DeleteServiceInput) error {
	f.deleteCalls++
	f.deletePlaneIDs = append(f.deletePlaneIDs, planeID)
	f.deleteInputs = append(f.deleteInputs, input)
	return nil
}

func mustCreateReadyPlane(t *testing.T, db testutil.ControlPlaneTestDatabase, name string) plane.Detail {
	t.Helper()
	ctx := context.Background()
	item, err := db.Store.CreatePlane(ctx, plane.CreateInput{
		Name:            name,
		DisplayName:     name,
		Provider:        "aliyun",
		Region:          "cn-beijing",
		GRPCEndpoint:    name + ".example.test:443",
		SouthboundToken: "southbound-" + name,
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
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
