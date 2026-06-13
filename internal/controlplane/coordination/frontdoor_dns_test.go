package coordination

import (
	"context"
	"strings"
	"testing"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

func TestSyncFrontDoorDNSEnsuresVerificationAndCNAME(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-a")
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneService{
		{
			ServiceId:                "svc_api",
			Host:                     "api.apps.example.com",
			DesiredState:             "active",
			FrontdoorCname:           "api.apps.example.com.cdn.example.net",
			FrontdoorVerifySubdomain: "_cdnauth.example.com",
			FrontdoorVerifyType:      "TXT",
			FrontdoorVerifyValue:     "verify-token",
		},
	})
	if err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
	}
	if len(dns.records) != 2 {
		t.Fatalf("records = %+v, want verification and CNAME", dns.records)
	}
	if dns.records[0].Host != "_cdnauth.example.com" || dns.records[0].RecordType != "TXT" || dns.records[0].Value != "verify-token" || dns.records[0].PlaneID != plane.ID || dns.records[0].ServiceID != "svc_api" {
		t.Fatalf("verification record = %+v", dns.records[0])
	}
	if dns.records[1].Host != "api.apps.example.com" || dns.records[1].RecordType != "CNAME" || dns.records[1].Value != "api.apps.example.com.cdn.example.net" || dns.records[1].PlaneID != plane.ID || dns.records[1].ServiceID != "svc_api" {
		t.Fatalf("cname record = %+v", dns.records[1])
	}
	if len(dns.deleted) != 1 || dns.deleted[0].Host != "_cdnauth.example.com" || dns.deleted[0].RecordType != "TXT" || dns.deleted[0].Value != "verify-token" || dns.deleted[0].PlaneID != plane.ID || dns.deleted[0].ServiceID != "svc_api" {
		t.Fatalf("deleted records = %+v, want verification TXT deletion", dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, plane.ID, "svc_api")
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
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		PlaneID:    plane.ID,
		ServiceID:  "svc_ready",
		Host:       "_cdnauth.ready.apps.example.com",
		RecordType: "TXT",
		Value:      "old-verify-token",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorVerification,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord returned error: %v", err)
	}
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneService{
		{
			ServiceId:      "svc_ready",
			Host:           "ready.apps.example.com",
			DesiredState:   "active",
			FrontdoorCname: "ready.apps.example.com.cdn.example.net",
		},
	}); err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
	}
	if len(dns.deleted) != 1 || dns.deleted[0].Host != "_cdnauth.ready.apps.example.com" || dns.deleted[0].Value != "old-verify-token" || dns.deleted[0].PlaneID != plane.ID || dns.deleted[0].ServiceID != "svc_ready" {
		t.Fatalf("deleted records = %+v, want stored verification deletion", dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, plane.ID, "svc_ready")
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].Purpose != controlplanestore.DNSRecordPurposeFrontDoorCNAME {
		t.Fatalf("DNS records after CNAME ready = %+v, want only CNAME", records)
	}
}

func TestSyncFrontDoorDNSSkipsDeletedService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createSyncTestPlane(t, db.Store, "plane-deleting-frontdoor")
	syncer := &PlaneSyncer{store: db.Store, dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncFrontDoorDNS(ctx, plane.ID, []*cloudplanev1.PlaneService{
		{
			ServiceId:                "svc_deleting",
			Host:                     "deleting-frontdoor.apps.example.com",
			DesiredState:             "deleted",
			FrontdoorCname:           "deleting-frontdoor.apps.example.com.cdn.example.net",
			FrontdoorVerifySubdomain: "_cdnauth.deleting-frontdoor.apps.example.com",
			FrontdoorVerifyType:      "TXT",
			FrontdoorVerifyValue:     "verify-token",
		},
	}); err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
	}
	if len(dns.records) != 0 || len(dns.deleted) != 0 {
		t.Fatalf("DNS operations = create %+v delete %+v, want none for deleting service", dns.records, dns.deleted)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, plane.ID, "svc_deleting")
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("stored DNS records = %+v, want none for deleting service", records)
	}
}

func TestDNSPodRecordAlreadyExistsIsIdempotent(t *testing.T) {
	t.Parallel()

	err := sdkerrors.NewTencentCloudSDKError("InvalidParameter.DomainRecordExist", "record exists", "req-test")
	if !isDNSPodRecordAlreadyExists(err) {
		t.Fatalf("expected DNSPod record-exists error to be idempotent")
	}
}

func TestDNSPodEnsureRecordRefusesUnownedRecord(t *testing.T) {
	t.Parallel()

	api := &fakeDNSPodAPI{records: []fakeDNSPodRecord{{
		id:         10,
		subdomain:  "api.apps",
		recordType: "CNAME",
		value:      "other.example.net",
		remark:     "manual record",
	}}}
	client := &dnsPodClient{client: api, domain: "example.com"}

	err := client.EnsureRecord(context.Background(), managedDNSRecord{
		PlaneID:    "pln_test",
		ServiceID:  "svc_test",
		Host:       "api.apps.example.com",
		RecordType: "CNAME",
		Value:      "api.apps.example.com.cdn.example.net",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	})
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("EnsureRecord error = %v, want ownership refusal", err)
	}
	if len(api.modified) != 0 || len(api.created) != 0 {
		t.Fatalf("DNSPod writes = create %+v modify %+v, want none", api.created, api.modified)
	}
}

func TestDNSPodEnsureRecordUpdatesOwnedRecord(t *testing.T) {
	t.Parallel()

	record := managedDNSRecord{
		PlaneID:    "pln_test",
		ServiceID:  "svc_test",
		Host:       "api.apps.example.com",
		RecordType: "CNAME",
		Value:      "api.apps.example.com.cdn.example.net",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}
	api := &fakeDNSPodAPI{records: []fakeDNSPodRecord{{
		id:         10,
		subdomain:  "api.apps",
		recordType: "CNAME",
		value:      "old.example.net",
		remark:     managedDNSRemark(record),
	}}}
	client := &dnsPodClient{client: api, domain: "example.com"}

	if err := client.EnsureRecord(context.Background(), record); err != nil {
		t.Fatalf("EnsureRecord returned error: %v", err)
	}
	if len(api.modified) != 1 || *api.modified[0].RecordId != 10 || *api.modified[0].Value != "api.apps.example.com.cdn.example.net" || *api.modified[0].Remark != managedDNSRemark(record) {
		t.Fatalf("modified requests = %+v, want owned record update", api.modified)
	}
}

func TestDNSPodDeleteRecordSkipsUnownedRecord(t *testing.T) {
	t.Parallel()

	api := &fakeDNSPodAPI{records: []fakeDNSPodRecord{{
		id:         10,
		subdomain:  "api.apps",
		recordType: "CNAME",
		value:      "api.apps.example.com.cdn.example.net",
		remark:     "manual record",
	}}}
	client := &dnsPodClient{client: api, domain: "example.com"}

	if err := client.DeleteRecord(context.Background(), managedDNSRecord{
		PlaneID:    "pln_test",
		ServiceID:  "svc_test",
		Host:       "api.apps.example.com",
		RecordType: "CNAME",
		Value:      "api.apps.example.com.cdn.example.net",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("DeleteRecord returned error: %v", err)
	}
	if len(api.deleted) != 0 {
		t.Fatalf("deleted records = %+v, want none", api.deleted)
	}
}

type fakeDNSClient struct {
	records []managedDNSRecord
	deleted []managedDNSRecord
}

func (f *fakeDNSClient) EnsureRecord(_ context.Context, record managedDNSRecord) error {
	f.records = append(f.records, normalizeManagedDNSRecord(record))
	return nil
}

func (f *fakeDNSClient) DeleteRecord(_ context.Context, record managedDNSRecord) error {
	f.deleted = append(f.deleted, normalizeManagedDNSRecord(record))
	return nil
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

type fakeDNSPodAPI struct {
	records  []fakeDNSPodRecord
	created  []*dnspod.CreateRecordRequest
	modified []*dnspod.ModifyRecordRequest
	deleted  []*dnspod.DeleteRecordRequest
}

type fakeDNSPodRecord struct {
	id         uint64
	subdomain  string
	recordType string
	value      string
	remark     string
}

func (f *fakeDNSPodAPI) CreateRecordWithContext(_ context.Context, req *dnspod.CreateRecordRequest) (*dnspod.CreateRecordResponse, error) {
	f.created = append(f.created, req)
	return &dnspod.CreateRecordResponse{Response: &dnspod.CreateRecordResponseParams{}}, nil
}

func (f *fakeDNSPodAPI) DeleteRecordWithContext(_ context.Context, req *dnspod.DeleteRecordRequest) (*dnspod.DeleteRecordResponse, error) {
	f.deleted = append(f.deleted, req)
	return &dnspod.DeleteRecordResponse{Response: &dnspod.DeleteRecordResponseParams{}}, nil
}

func (f *fakeDNSPodAPI) DescribeRecordListWithContext(_ context.Context, req *dnspod.DescribeRecordListRequest) (*dnspod.DescribeRecordListResponse, error) {
	items := make([]*dnspod.RecordListItem, 0)
	subdomain := stringPtrValue(req.Subdomain)
	recordType := strings.ToUpper(stringPtrValue(req.RecordType))
	for _, record := range f.records {
		if record.subdomain != subdomain || strings.ToUpper(record.recordType) != recordType {
			continue
		}
		items = append(items, &dnspod.RecordListItem{
			RecordId: tccommon.Uint64Ptr(record.id),
			Value:    tccommon.StringPtr(record.value),
			Remark:   tccommon.StringPtr(record.remark),
		})
	}
	return &dnspod.DescribeRecordListResponse{
		Response: &dnspod.DescribeRecordListResponseParams{RecordList: items},
	}, nil
}

func (f *fakeDNSPodAPI) ModifyRecordWithContext(_ context.Context, req *dnspod.ModifyRecordRequest) (*dnspod.ModifyRecordResponse, error) {
	f.modified = append(f.modified, req)
	return &dnspod.ModifyRecordResponse{Response: &dnspod.ModifyRecordResponseParams{}}, nil
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
