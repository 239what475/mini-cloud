package coordination

import (
	"context"
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
			{PlanId: "plan-a"},
			{PlanId: "plan-b"},
			{PlanId: "plan-c"},
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
			{PlanId: "plan-a"},
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

func TestApplyFrontDoorDNSEnsuresVerificationAndCNAME(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-a")
	service := createSyncTestService(t, db.Store, plane.ID, "api", "api.apps.example.com")
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	err := syncer.applyFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneFrontDoorDomain{
		{
			Host:            "api.apps.example.com",
			Cname:           "api.apps.example.com.cdn.example.net",
			VerifySubdomain: "_cdnauth.example.com",
			VerifyType:      "TXT",
			VerifyValue:     "verify-token",
		},
	})
	if err != nil {
		t.Fatalf("applyFrontDoorDNS returned error: %v", err)
	}
	if len(dns.records) != 2 {
		t.Fatalf("records = %+v, want verification and CNAME", dns.records)
	}
	if dns.records[0].host != "_cdnauth.example.com" || dns.records[0].recordType != "TXT" || dns.records[0].value != "verify-token" {
		t.Fatalf("verification record = %+v", dns.records[0])
	}
	if dns.records[1].host != "api.apps.example.com" || dns.records[1].recordType != "CNAME" || dns.records[1].value != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("cname record = %+v", dns.records[1])
	}
	if len(dns.deleted) != 1 || dns.deleted[0].host != "_cdnauth.example.com" || dns.deleted[0].recordType != "TXT" || dns.deleted[0].value != "verify-token" {
		t.Fatalf("deleted records = %+v, want verification TXT deletion", dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].Host != "api.apps.example.com" || records[0].RecordType != "CNAME" {
		t.Fatalf("stored DNS records = %+v, want only CNAME after verification cleanup", records)
	}
}

func TestApplyFrontDoorDNSDeletesStoredVerificationAfterCNAMEReady(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-cname-ready")
	service := createSyncTestService(t, db.Store, plane.ID, "ready", "ready.apps.example.com")
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  service.Metadata.ID,
		Host:       "_cdnauth.ready.apps.example.com",
		RecordType: "TXT",
		Value:      "old-verify-token",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorVerification,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord returned error: %v", err)
	}
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.applyFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneFrontDoorDomain{
		{
			Host:  "ready.apps.example.com",
			Cname: "ready.apps.example.com.cdn.example.net",
		},
	}); err != nil {
		t.Fatalf("applyFrontDoorDNS returned error: %v", err)
	}
	if len(dns.deleted) != 1 || dns.deleted[0].host != "_cdnauth.ready.apps.example.com" || dns.deleted[0].value != "old-verify-token" {
		t.Fatalf("deleted records = %+v, want stored verification deletion", dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].Purpose != controlplanestore.DNSRecordPurposeFrontDoorCNAME {
		t.Fatalf("DNS records after CNAME ready = %+v, want only CNAME", records)
	}
}

func TestApplyExecutionSnapshotsDeletesServiceDNSAfterRemoteDelete(t *testing.T) {
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

	if err := syncer.applyExecutionSnapshots(ctx, plane.ID, []*cloudplanev1.PlaneExecutionSnapshot{
		{
			PlanId:            "delete-plan",
			ServiceId:         deleting.Metadata.ID,
			ServiceGeneration: deleting.Metadata.Generation,
			Status:            planeExecutionStatusSucceeded,
			ObservedAt:        timestamppb.New(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("applyExecutionSnapshots returned error: %v", err)
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

type fakeDNSClient struct {
	records []fakeDNSRecord
	deleted []fakeDeletedDNSRecord
}

type fakeDNSRecord struct {
	host       string
	recordType string
	value      string
}

type fakeDeletedDNSRecord struct {
	host       string
	recordType string
	value      string
}

func (f *fakeDNSClient) EnsureRecord(_ context.Context, host string, recordType string, value string) error {
	f.records = append(f.records, fakeDNSRecord{host: host, recordType: recordType, value: value})
	return nil
}

func (f *fakeDNSClient) DeleteRecord(_ context.Context, host string, recordType string, value string) error {
	f.deleted = append(f.deleted, fakeDeletedDNSRecord{host: host, recordType: recordType, value: value})
	return nil
}

func TestBuildNodeInventoryIncludesElasticNodeSource(t *testing.T) {
	observedAt := time.Now().UTC()
	input := buildNodeInventory(&cloudplanev1.PlaneSnapshot{
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			ObservedAt: timestamppb.New(observedAt),
			Nodes: []*cloudplanev1.PlaneNode{
				{
					NodeId:              "node-a",
					Name:                "node-a",
					Status:              "ready",
					Schedulable:         true,
					Elastic:             true,
					CpuMilliAllocatable: 1000,
					MemoryMiAllocatable: 1024,
				},
			},
		},
	})

	if !input.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %v, want %v", input.ObservedAt, observedAt)
	}
	if len(input.Nodes) != 1 || !input.Nodes[0].Elastic {
		t.Fatalf("node inventory nodes = %+v, want elastic node", input.Nodes)
	}
}

func TestServiceStatusFromExecutionSnapshotRunningPromotesCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseReady {
		t.Fatalf("status = %+v, want ready", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g2" || status.Run.LatestRunID != "svc-api-g2" {
		t.Fatalf("run ids = %+v, want current/latest g2", status.Run)
	}
	if status.Run.Phase != model.RunPhaseRunning {
		t.Fatalf("run = %+v, want running", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotFailedDoesNotRollbackCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseDegraded {
		t.Fatalf("status = %+v, want degraded", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != model.RunPhaseFailed {
		t.Fatalf("run = %+v, want latest failed g2", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotProgressingKeepsCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("status = %+v, want progressing", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != model.RunPhaseDispatching {
		t.Fatalf("run = %+v, want latest g2 dispatching", status.Run)
	}
}
