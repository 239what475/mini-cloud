package coordination

import (
	"context"
	"testing"

	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"
)

func TestSyncFrontDoorDNSEnsuresVerificationAndCNAME(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-a")
	service := createSyncTestService(t, db.Store, plane.ID, "api", "api.apps.example.com")
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneFrontDoorDomain{
		{
			Host:            "api.apps.example.com",
			Cname:           "api.apps.example.com.cdn.example.net",
			VerifySubdomain: "_cdnauth.example.com",
			VerifyType:      "TXT",
			VerifyValue:     "verify-token",
		},
	})
	if err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
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

func TestSyncFrontDoorDNSDeletesStoredVerificationAfterCNAMEReady(t *testing.T) {
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

	if err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneFrontDoorDomain{
		{
			Host:  "ready.apps.example.com",
			Cname: "ready.apps.example.com.cdn.example.net",
		},
	}); err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
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

func TestSyncFrontDoorDNSSkipsDeletingService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-deleting-frontdoor")
	service := createSyncTestService(t, db.Store, plane.ID, "deleting-frontdoor", "deleting-frontdoor.apps.example.com")
	if _, err := db.Store.MarkServiceDeletionRequested(ctx, service.Metadata.ID); err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneFrontDoorDomain{
		{
			Host:            "deleting-frontdoor.apps.example.com",
			Cname:           "deleting-frontdoor.apps.example.com.cdn.example.net",
			VerifySubdomain: "_cdnauth.deleting-frontdoor.apps.example.com",
			VerifyType:      "TXT",
			VerifyValue:     "verify-token",
		},
	}); err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
	}
	if len(dns.records) != 0 || len(dns.deleted) != 0 {
		t.Fatalf("DNS operations = create %+v delete %+v, want none for deleting service", dns.records, dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("stored DNS records = %+v, want none for deleting service", records)
	}
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
