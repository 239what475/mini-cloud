package coordination

import (
	"context"
	"strings"
	"testing"

	controlplaneconfig "mini-cloud/internal/controlplane/config"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/transport"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

func TestSyncFrontDoorDNSEnsuresVerificationAndCNAME(t *testing.T) {
	ctx := context.Background()
	syncer := &PlaneSyncer{dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	err := syncer.syncFrontDoorDNS(ctx, "pln_test", []*cloudplanev1.PlaneService{
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
	if dns.records[0].Host != "_cdnauth.example.com" || dns.records[0].RecordType != "TXT" || dns.records[0].Value != "verify-token" || dns.records[0].PlaneID != "pln_test" || dns.records[0].ServiceID != "svc_api" {
		t.Fatalf("verification record = %+v", dns.records[0])
	}
	if dns.records[1].Host != "api.apps.example.com" || dns.records[1].RecordType != "CNAME" || dns.records[1].Value != "api.apps.example.com.cdn.example.net" || dns.records[1].PlaneID != "pln_test" || dns.records[1].ServiceID != "svc_api" {
		t.Fatalf("cname record = %+v", dns.records[1])
	}
	if len(dns.deleted) != 1 || dns.deleted[0].Host != "_cdnauth.example.com" || dns.deleted[0].RecordType != "TXT" || dns.deleted[0].Value != "verify-token" || dns.deleted[0].PlaneID != "pln_test" || dns.deleted[0].ServiceID != "svc_api" {
		t.Fatalf("deleted records = %+v, want verification TXT deletion", dns.deleted)
	}
}

func TestSyncFrontDoorDNSSweepsVerificationAfterCNAMEReady(t *testing.T) {
	ctx := context.Background()
	syncer := &PlaneSyncer{dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncFrontDoorDNS(ctx, "pln_test", []*cloudplanev1.PlaneService{
		{
			ServiceId:      "svc_ready",
			Host:           "ready.apps.example.com",
			DesiredState:   "active",
			FrontdoorCname: "ready.apps.example.com.cdn.example.net",
		},
	}); err != nil {
		t.Fatalf("syncFrontDoorDNS returned error: %v", err)
	}
	if len(dns.deletedServices) != 1 || dns.deletedServices[0].PlaneID != "pln_test" || dns.deletedServices[0].ServiceID != "svc_ready" || dns.deletedServices[0].Purpose != dnsRecordPurposeFrontDoorVerification {
		t.Fatalf("deleted service records = %+v, want verification sweep", dns.deletedServices)
	}
}

func TestSyncFrontDoorDNSSkipsDeletedService(t *testing.T) {
	ctx := context.Background()
	syncer := &PlaneSyncer{dns: &fakeDNSClient{}}
	dns := syncer.dns.(*fakeDNSClient)

	if err := syncer.syncFrontDoorDNS(ctx, "pln_test", []*cloudplanev1.PlaneService{
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
	if len(dns.records) != 0 || len(dns.deleted) != 0 || len(dns.deletedServices) != 0 {
		t.Fatalf("DNS operations = create %+v delete %+v sweep %+v, want none for deleting service", dns.records, dns.deleted, dns.deletedServices)
	}
}

func TestDNSPodRecordAlreadyExistsIsIdempotent(t *testing.T) {
	t.Parallel()

	err := sdkerrors.NewTencentCloudSDKError("InvalidParameter.DomainRecordExist", "record exists", "req-test")
	if !isDNSPodRecordAlreadyExists(err) {
		t.Fatalf("expected DNSPod record-exists error to be idempotent")
	}
}

func TestDNSPodCredentialRejectsPartialStaticCredential(t *testing.T) {
	t.Parallel()

	if _, err := staticDNSPodCredential(controlplaneconfig.DNSPodConfig{SecretID: "sid"}); err == nil {
		t.Fatalf("staticDNSPodCredential returned nil error, want partial credential validation error")
	}
}

func TestDNSPodCredentialUsesStaticCredentialWhenConfigured(t *testing.T) {
	t.Parallel()

	credential, err := staticDNSPodCredential(controlplaneconfig.DNSPodConfig{SecretID: "sid", SecretKey: "skey", Token: "token"})
	if err != nil {
		t.Fatalf("staticDNSPodCredential returned error: %v", err)
	}
	if credential == nil {
		t.Fatalf("staticDNSPodCredential returned nil credential")
	}
}

func TestDNSPodClientUsesTencentCredentialFromContext(t *testing.T) {
	t.Parallel()

	ctx := transport.ContextWithTencentCredential(context.Background(), transport.TencentCredential{
		SecretID:     "sid",
		SecretKey:    "skey",
		SessionToken: "session-token",
	})
	client := &dnsPodClient{domain: "example.com"}
	credential, err := client.credential(ctx)
	if err != nil {
		t.Fatalf("credential returned error: %v", err)
	}
	secretID, secretKey, token := credential.GetCredential()
	if secretID != "sid" || secretKey != "skey" || token != "session-token" {
		t.Fatalf("credential = %q/%q/%q, want SCF request credential", secretID, secretKey, token)
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
		Purpose:    dnsRecordPurposeFrontDoorCNAME,
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
		Purpose:    dnsRecordPurposeFrontDoorCNAME,
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
		Purpose:    dnsRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("DeleteRecord returned error: %v", err)
	}
	if len(api.deleted) != 0 {
		t.Fatalf("deleted records = %+v, want none", api.deleted)
	}
}

func TestDNSPodDeleteServiceRecordsDeletesOnlyOwnedRecords(t *testing.T) {
	t.Parallel()

	cname := managedDNSRecord{
		PlaneID:    "pln_test",
		ServiceID:  "svc_test",
		Host:       "api.apps.example.com",
		RecordType: "CNAME",
		Value:      "api.apps.example.com.cdn.example.net",
		Purpose:    dnsRecordPurposeFrontDoorCNAME,
	}
	verify := managedDNSRecord{
		PlaneID:    "pln_test",
		ServiceID:  "svc_test",
		Host:       "_cdnauth.api.apps.example.com",
		RecordType: "TXT",
		Value:      "verify",
		Purpose:    dnsRecordPurposeFrontDoorVerification,
	}
	api := &fakeDNSPodAPI{records: []fakeDNSPodRecord{
		{id: 10, subdomain: "api.apps", recordType: "CNAME", value: cname.Value, remark: managedDNSRemark(cname)},
		{id: 11, subdomain: "_cdnauth.api.apps", recordType: "TXT", value: verify.Value, remark: managedDNSRemark(verify)},
		{id: 12, subdomain: "manual.apps", recordType: "CNAME", value: "manual.example.net", remark: "manual"},
	}}
	client := &dnsPodClient{client: api, domain: "example.com"}

	if err := client.DeleteServiceRecords(context.Background(), "pln_test", "svc_test", ""); err != nil {
		t.Fatalf("DeleteServiceRecords returned error: %v", err)
	}
	if len(api.deleted) != 2 || *api.deleted[0].RecordId != 10 || *api.deleted[1].RecordId != 11 {
		t.Fatalf("deleted records = %+v, want owned service records", api.deleted)
	}
}

type deletedServiceRecords struct {
	PlaneID   string
	ServiceID string
	Purpose   string
}

type fakeDNSClient struct {
	records         []managedDNSRecord
	deleted         []managedDNSRecord
	deletedServices []deletedServiceRecords
}

func (f *fakeDNSClient) EnsureRecord(_ context.Context, record managedDNSRecord) error {
	f.records = append(f.records, normalizeManagedDNSRecord(record))
	return nil
}

func (f *fakeDNSClient) DeleteRecord(_ context.Context, record managedDNSRecord) error {
	f.deleted = append(f.deleted, normalizeManagedDNSRecord(record))
	return nil
}

func (f *fakeDNSClient) DeleteServiceRecords(_ context.Context, planeID string, serviceID string, purpose string) error {
	f.deletedServices = append(f.deletedServices, deletedServiceRecords{PlaneID: planeID, ServiceID: serviceID, Purpose: purpose})
	return nil
}

func (f *fakeDNSClient) Check(context.Context) error {
	return nil
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
		if subdomain != "" && record.subdomain != subdomain {
			continue
		}
		if recordType != "" && strings.ToUpper(record.recordType) != recordType {
			continue
		}
		items = append(items, &dnspod.RecordListItem{
			RecordId: tccommon.Uint64Ptr(record.id),
			Name:     tccommon.StringPtr(record.subdomain),
			Type:     tccommon.StringPtr(record.recordType),
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
