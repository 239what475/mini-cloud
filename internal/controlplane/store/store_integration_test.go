package store_test

import (
	"context"
	"errors"
	"testing"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)

	created, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "aliyun-bj-primary",
		DisplayName:  "Aliyun Beijing Primary",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "grpc://plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if created.Status.Status != model.StatusSyncing {
		t.Fatalf("initial status = %s, want syncing", created.Status.Status)
	}
	if created.GRPCEndpoint != "plane-a.example.com:443" {
		t.Fatalf("grpcEndpoint = %q, want normalized target", created.GRPCEndpoint)
	}

	if err := db.Store.UpdatePlaneStatus(ctx, created.ID, controlplanestore.UpdatePlaneStatusInput{
		Status:  model.StatusReady,
		Message: "snapshot healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	got, err := db.Store.GetPlane(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	if got.Status.Status != model.StatusReady || got.Status.Message != "snapshot healthy" {
		t.Fatalf("status = %+v, want ready snapshot healthy", got.Status)
	}

	ids, err := db.Store.ListPlaneIDs(ctx)
	if err != nil {
		t.Fatalf("ListPlaneIDs returned error: %v", err)
	}
	if len(ids) != 1 || ids[0] != created.ID {
		t.Fatalf("plane ids = %+v, want created plane", ids)
	}

	if err := db.Store.DeletePlane(ctx, created.ID); err != nil {
		t.Fatalf("DeletePlane returned error: %v", err)
	}
	if _, err := db.Store.GetPlane(ctx, created.ID); !errors.Is(err, controlplanestore.ErrPlaneNotFound) {
		t.Fatalf("GetPlane after delete error = %v, want ErrPlaneNotFound", err)
	}
}

func TestIntegrationRegisterPlaneRefreshDoesNotResetCurrentStatus(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)

	created, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "refresh-plane",
		DisplayName:  "Refresh Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "refresh-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, created.ID, controlplanestore.UpdatePlaneStatusInput{
		Status:  model.StatusReady,
		Message: "last snapshot healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	refreshed, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "refresh-plane",
		DisplayName:  "Refresh Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "refresh-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane(refresh) returned error: %v", err)
	}
	if refreshed.ID != created.ID {
		t.Fatalf("refreshed plane id = %q, want existing %q", refreshed.ID, created.ID)
	}
	if refreshed.Status.Status != model.StatusReady || refreshed.Status.Message != "last snapshot healthy" {
		t.Fatalf("refreshed status = %+v, want existing ready status", refreshed.Status)
	}
	if refreshed.Status.LastHeartbeatAt == nil {
		t.Fatalf("refreshed status did not update heartbeat")
	}
}

func TestIntegrationServiceDNSRecords(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createPlane(t, db)

	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		PlaneID:    plane.ID,
		ServiceID:  "svc_dns",
		Host:       "api.apps.example.test",
		RecordType: "CNAME",
		Value:      "api.apps.example.test.cdn.example.net.",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord returned error: %v", err)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, plane.ID, "svc_dns")
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].PlaneID != plane.ID || records[0].Host != "api.apps.example.test" || records[0].Value != "api.apps.example.test.cdn.example.net" {
		t.Fatalf("DNS records = %+v, want saved record", records)
	}
	if err := db.Store.DeleteServiceDNSRecords(ctx, plane.ID, "svc_dns"); err != nil {
		t.Fatalf("DeleteServiceDNSRecords returned error: %v", err)
	}
	records, err = db.Store.ListServiceDNSRecords(ctx, plane.ID, "svc_dns")
	if err != nil {
		t.Fatalf("ListServiceDNSRecords after delete returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("DNS records after delete = %+v, want none", records)
	}
}

func createPlane(t *testing.T, db testutil.ControlPlaneTestDatabase) model.PlaneDetail {
	t.Helper()
	plane, err := db.Store.RegisterPlane(context.Background(), controlplanestore.RegisterPlaneInput{
		Name:         "test-plane",
		DisplayName:  "Test Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "test-plane.example.test:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	return plane
}
