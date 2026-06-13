package coordination

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDerivePlaneStatusReadyAndDegraded(t *testing.T) {
	planeDetail := model.PlaneDetail{
		Plane: model.Plane{
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
	}

	readyStatus, readyMessage := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			Nodes: []*cloudplanev1.PlaneNode{
				{Status: "ready"},
			},
		},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{ServiceId: "svc-a"},
			{ServiceId: "svc-b"},
			{ServiceId: "svc-c"},
		},
	})
	if readyStatus != model.StatusReady {
		t.Fatalf("ready status = %v, want ready", readyStatus)
	}
	if !strings.Contains(readyMessage, "sync healthy") {
		t.Fatalf("ready message = %q", readyMessage)
	}

	degradedStatus, degradedMessage := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Provider: "tencent",
			Region:   "ap-beijing",
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			Nodes: []*cloudplanev1.PlaneNode{
				{Status: "ready"},
				{Status: "offline"},
			},
		},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{ServiceId: "svc-a"},
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: 1,
		},
	})
	if degradedStatus != model.StatusDegraded {
		t.Fatalf("degraded status = %v, want degraded", degradedStatus)
	}
	if !strings.Contains(degradedMessage, "provider mismatch") || !strings.Contains(degradedMessage, "reliability alert") {
		t.Fatalf("unexpected degraded message: %q", degradedMessage)
	}
}

func TestSyncExecutionSnapshotsDeletesServiceDNSAfterRemoteDelete(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-delete-dns")
	service := createSyncTestService(t, db.Store, plane.ID, "gone", "gone.apps.example.com")
	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  deleting.Metadata.ID,
		Host:       "gone.apps.example.com",
		RecordType: "CNAME",
		Value:      "gone.apps.example.com.cdn.dnsv1.com",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord(CNAME) returned error: %v", err)
	}
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  deleting.Metadata.ID,
		Host:       "_cdnauth.gone.apps.example.com",
		RecordType: "TXT",
		Value:      "verify-token",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorVerification,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord(TXT) returned error: %v", err)
	}
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncExecutionSnapshots(ctx, plane.ID, []*cloudplanev1.PlaneExecutionSnapshot{
		{
			ServiceId:         deleting.Metadata.ID,
			ServiceGeneration: deleting.Metadata.Generation,
			Status:            planeExecutionStatusSucceeded,
			ObservedAt:        timestamppb.New(time.Now().UTC()),
		},
	}, nil); err != nil {
		t.Fatalf("syncExecutionSnapshots returned error: %v", err)
	}
	if len(dns.deleted) != 2 {
		t.Fatalf("deleted DNS records = %+v, want CNAME and TXT", dns.deleted)
	}
	if _, err := db.Store.GetService(ctx, service.Metadata.ID); err == nil {
		t.Fatalf("GetService after delete returned nil error, want not found")
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("DNS records after delete = %+v, want none", records)
	}
}

func TestSyncExecutionSnapshotsWaitsForFrontDoorRemovalBeforeDeletingService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-delete-waits-frontdoor")
	service := createSyncTestService(t, db.Store, plane.ID, "wait-frontdoor", "wait-frontdoor.apps.example.com")
	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  deleting.Metadata.ID,
		Host:       "wait-frontdoor.apps.example.com",
		RecordType: "CNAME",
		Value:      "wait-frontdoor.apps.example.com.cdn.dnsv1.com",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord(CNAME) returned error: %v", err)
	}
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncExecutionSnapshots(ctx, plane.ID, []*cloudplanev1.PlaneExecutionSnapshot{
		{
			ServiceId:         deleting.Metadata.ID,
			ServiceGeneration: deleting.Metadata.Generation,
			Status:            planeExecutionStatusSucceeded,
			ObservedAt:        timestamppb.New(time.Now().UTC()),
		},
	}, []*cloudplanev1.PlaneFrontDoorDomain{
		{Host: "wait-frontdoor.apps.example.com", Cname: "wait-frontdoor.apps.example.com.cdn.dnsv1.com"},
	}); err != nil {
		t.Fatalf("syncExecutionSnapshots returned error: %v", err)
	}
	if len(dns.deleted) != 0 {
		t.Fatalf("deleted DNS records = %+v, want none while frontdoor is still reported", dns.deleted)
	}
	if _, err := db.Store.GetService(ctx, service.Metadata.ID); err != nil {
		t.Fatalf("GetService after pending frontdoor cleanup returned error: %v", err)
	}

	if err := syncer.syncExecutionSnapshots(ctx, plane.ID, []*cloudplanev1.PlaneExecutionSnapshot{
		{
			ServiceId:         deleting.Metadata.ID,
			ServiceGeneration: deleting.Metadata.Generation,
			Status:            planeExecutionStatusSucceeded,
			ObservedAt:        timestamppb.New(time.Now().UTC()),
		},
	}, nil); err != nil {
		t.Fatalf("syncExecutionSnapshots after frontdoor cleanup returned error: %v", err)
	}
	if len(dns.deleted) != 1 {
		t.Fatalf("deleted DNS records = %+v, want CNAME after frontdoor disappears", dns.deleted)
	}
	if _, err := db.Store.GetService(ctx, service.Metadata.ID); !errors.Is(err, controlplanestore.ErrServiceNotFound) {
		t.Fatalf("GetService after frontdoor cleanup error = %v, want service not found", err)
	}
}

func createSyncTestPlane(t *testing.T, stores *controlplanestore.Store, name string) model.PlaneDetail {
	t.Helper()
	item, err := stores.RegisterPlane(context.Background(), controlplanestore.RegisterPlaneInput{
		Name:         name,
		DisplayName:  name,
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: name + ".example.test:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	return item
}

func createSyncTestService(t *testing.T, stores *controlplanestore.Store, planeID string, name string, host string) model.Service {
	t.Helper()
	item, err := stores.CreateService(context.Background(), controlplanestore.CreateServiceInput{
		Name:        name,
		DisplayName: name,
		Host:        host,
		Spec: model.ServiceSpec{
			PlaneID:       planeID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	return item
}

func TestServiceStatusFromExecutionSnapshotRunning(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseReady {
		t.Fatalf("status = %+v, want ready", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseRunning {
		t.Fatalf("run = %+v, want running", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotFailed(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseDegraded {
		t.Fatalf("status = %+v, want degraded", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseFailed {
		t.Fatalf("run = %+v, want failed", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotProgressing(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("status = %+v, want progressing", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseDispatching {
		t.Fatalf("run = %+v, want dispatching", status.Run)
	}
}
