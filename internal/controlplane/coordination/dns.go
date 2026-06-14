package coordination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/config"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

const (
	dnsRecordTTL         uint64 = 600
	dnsPodRequestTimeout        = 15 * time.Second
	dnsRecordOwner       string = "mini-cloud"
)

type dnsClient interface {
	EnsureRecord(context.Context, managedDNSRecord) error
	DeleteRecord(context.Context, managedDNSRecord) error
	DeleteServiceRecords(context.Context, string, string, string) error
	Check(context.Context) error
}

type managedDNSRecord struct {
	PlaneID    string
	ServiceID  string
	Host       string
	RecordType string
	Value      string
	Purpose    string
}

type dnsPodAPI interface {
	CreateRecordWithContext(context.Context, *dnspod.CreateRecordRequest) (*dnspod.CreateRecordResponse, error)
	DeleteRecordWithContext(context.Context, *dnspod.DeleteRecordRequest) (*dnspod.DeleteRecordResponse, error)
	DescribeRecordListWithContext(context.Context, *dnspod.DescribeRecordListRequest) (*dnspod.DescribeRecordListResponse, error)
	ModifyRecordWithContext(context.Context, *dnspod.ModifyRecordRequest) (*dnspod.ModifyRecordResponse, error)
}

type dnsPodClient struct {
	client dnsPodAPI
	domain string
}

func newDNSPodClient(cfg config.DNSPodConfig) (*dnsPodClient, error) {
	if strings.TrimSpace(cfg.Domain) == "" {
		return nil, fmt.Errorf("dns.dnspod.domain is required")
	}
	return &dnsPodClient{domain: cleanDNSDomain(cfg.Domain)}, nil
}

func (c *dnsPodClient) api() (dnsPodAPI, error) {
	if c.client != nil {
		return c.client, nil
	}
	credential, err := c.credential()
	if err != nil {
		return nil, err
	}
	profile := tcprofile.NewClientProfile()
	profile.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	profile.HttpProfile.ReqTimeout = int(dnsPodRequestTimeout / time.Second)
	client, err := dnspod.NewClient(credential, "", profile)
	if err != nil {
		return nil, fmt.Errorf("create DNSPod client: %w", err)
	}
	return client, nil
}

func (c *dnsPodClient) credential() (tccommon.CredentialIface, error) {
	credential, err := tccommon.DefaultProviderChain().GetCredential()
	if err != nil {
		return nil, fmt.Errorf("load DNSPod credential from Tencent default provider chain: %w", err)
	}
	return credential, nil
}

func (c *dnsPodClient) EnsureRecord(ctx context.Context, record managedDNSRecord) error {
	record = normalizeManagedDNSRecord(record)
	if err := validateManagedDNSRecord(record); err != nil {
		return err
	}
	subdomain, err := c.subdomain(record.Host)
	if err != nil {
		return err
	}

	records, err := c.recordsForSubdomain(ctx, subdomain, record.RecordType)
	if err != nil {
		return err
	}
	if err := c.ensureAgainstExistingRecords(ctx, subdomain, record, records); err != errDNSRecordMissing {
		return err
	}
	if err := c.createRecord(ctx, subdomain, record.RecordType, record.Value, managedDNSRemark(record)); err != nil {
		if !isDNSPodRecordAlreadyExists(err) {
			return err
		}
		records, listErr := c.recordsForSubdomain(ctx, subdomain, record.RecordType)
		if listErr != nil {
			return listErr
		}
		return c.ensureAgainstExistingRecords(ctx, subdomain, record, records)
	}
	return nil
}

func (c *dnsPodClient) DeleteRecord(ctx context.Context, record managedDNSRecord) error {
	record = normalizeManagedDNSRecord(record)
	if err := validateManagedDNSRecord(record); err != nil {
		return err
	}
	subdomain, err := c.subdomain(record.Host)
	if err != nil {
		return err
	}
	records, err := c.recordsForSubdomain(ctx, subdomain, record.RecordType)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if !dnsRecordOwnedBy(existing.remark, record) {
			continue
		}
		if record.Value != "" && cleanDNSRecordValue(record.RecordType, existing.value) != record.Value {
			continue
		}
		if err := c.deleteRecord(ctx, existing.id); err != nil {
			return err
		}
	}
	return nil
}

func (c *dnsPodClient) DeleteServiceRecords(ctx context.Context, planeID string, serviceID string, purpose string) error {
	planeID = strings.TrimSpace(planeID)
	serviceID = strings.TrimSpace(serviceID)
	purpose = strings.TrimSpace(purpose)
	if planeID == "" || serviceID == "" {
		return fmt.Errorf("managed DNS record deletion requires planeID and serviceID")
	}
	records, err := c.recordsForDomain(ctx)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.id == 0 || existing.subdomain == "" || existing.recordType == "" {
			continue
		}
		record := managedDNSRecord{
			PlaneID:    planeID,
			ServiceID:  serviceID,
			Host:       c.host(existing.subdomain),
			RecordType: existing.recordType,
			Value:      existing.value,
			Purpose:    purposeFromRemark(existing.remark),
		}
		if purpose != "" {
			record.Purpose = purpose
		}
		if !dnsRecordOwnedBy(existing.remark, record) {
			continue
		}
		if err := c.deleteRecord(ctx, existing.id); err != nil {
			return err
		}
	}
	return nil
}

func (c *dnsPodClient) Check(ctx context.Context) error {
	_, err := c.recordsForDomain(ctx)
	return err
}

type dnsRecord struct {
	id         uint64
	subdomain  string
	recordType string
	value      string
	remark     string
}

var errDNSRecordMissing = errors.New("DNS record is missing")

func NewDNSClient(cfg config.DNSPodConfig) (dnsClient, error) {
	return newDNSPodClient(cfg)
}

func (c *dnsPodClient) ensureAgainstExistingRecords(ctx context.Context, subdomain string, record managedDNSRecord, records []dnsRecord) error {
	remark := managedDNSRemark(record)
	for _, existing := range records {
		if !dnsRecordOwnedBy(existing.remark, record) {
			return fmt.Errorf("DNS record %s %s already exists and is not owned by mini-cloud service %s on plane %s", record.Host, record.RecordType, record.ServiceID, record.PlaneID)
		}
		if cleanDNSRecordValue(record.RecordType, existing.value) == record.Value && strings.TrimSpace(existing.remark) == remark {
			return nil
		}
		return c.modifyRecord(ctx, existing.id, subdomain, record.RecordType, record.Value, remark)
	}
	return errDNSRecordMissing
}

func (c *dnsPodClient) recordsForSubdomain(ctx context.Context, subdomain string, recordType string) ([]dnsRecord, error) {
	api, err := c.api()
	if err != nil {
		return nil, err
	}
	limit := uint64(100)
	errorOnEmpty := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.Subdomain = tccommon.StringPtr(subdomain)
	req.RecordType = tccommon.StringPtr(recordType)
	req.Limit = &limit
	req.ErrorOnEmpty = &errorOnEmpty

	resp, err := api.DescribeRecordListWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	out := make([]dnsRecord, 0, len(resp.Response.RecordList))
	for _, item := range resp.Response.RecordList {
		if item == nil || item.RecordId == nil || item.Value == nil {
			continue
		}
		remark := ""
		if item.Remark != nil {
			remark = *item.Remark
		}
		out = append(out, dnsRecord{id: *item.RecordId, subdomain: subdomain, recordType: recordType, value: *item.Value, remark: remark})
	}
	return out, nil
}

func (c *dnsPodClient) recordsForDomain(ctx context.Context) ([]dnsRecord, error) {
	api, err := c.api()
	if err != nil {
		return nil, err
	}
	limit := uint64(3000)
	errorOnEmpty := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.Limit = &limit
	req.ErrorOnEmpty = &errorOnEmpty

	resp, err := api.DescribeRecordListWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	out := make([]dnsRecord, 0, len(resp.Response.RecordList))
	for _, item := range resp.Response.RecordList {
		if item == nil || item.RecordId == nil || item.Value == nil || item.Name == nil || item.Type == nil {
			continue
		}
		remark := ""
		if item.Remark != nil {
			remark = *item.Remark
		}
		out = append(out, dnsRecord{
			id:         *item.RecordId,
			subdomain:  strings.Trim(strings.ToLower(strings.TrimSpace(*item.Name)), "."),
			recordType: strings.ToUpper(strings.TrimSpace(*item.Type)),
			value:      *item.Value,
			remark:     remark,
		})
	}
	return out, nil
}

func (c *dnsPodClient) createRecord(ctx context.Context, subdomain string, recordType string, value string, remark string) error {
	api, err := c.api()
	if err != nil {
		return err
	}
	line := "默认"
	req := dnspod.NewCreateRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(value)
	req.TTL = tccommon.Uint64Ptr(dnsRecordTTL)
	req.Remark = tccommon.StringPtr(remark)
	_, err = api.CreateRecordWithContext(ctx, req)
	return err
}

func (c *dnsPodClient) modifyRecord(ctx context.Context, recordID uint64, subdomain string, recordType string, value string, remark string) error {
	api, err := c.api()
	if err != nil {
		return err
	}
	line := "默认"
	req := dnspod.NewModifyRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(recordID)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(value)
	req.TTL = tccommon.Uint64Ptr(dnsRecordTTL)
	req.Remark = tccommon.StringPtr(remark)
	_, err = api.ModifyRecordWithContext(ctx, req)
	return err
}

func (c *dnsPodClient) deleteRecord(ctx context.Context, recordID uint64) error {
	api, err := c.api()
	if err != nil {
		return err
	}
	req := dnspod.NewDeleteRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(recordID)
	_, err = api.DeleteRecordWithContext(ctx, req)
	return err
}

func (c *dnsPodClient) subdomain(host string) (string, error) {
	if host == c.domain {
		return "@", nil
	}
	suffix := "." + c.domain
	if !strings.HasSuffix(host, suffix) {
		return "", fmt.Errorf("host %q is outside DNSPod domain %q", host, c.domain)
	}
	return strings.TrimSuffix(host, suffix), nil
}

func (c *dnsPodClient) host(subdomain string) string {
	subdomain = strings.Trim(strings.ToLower(strings.TrimSpace(subdomain)), ".")
	if subdomain == "" || subdomain == "@" {
		return c.domain
	}
	return subdomain + "." + c.domain
}

func cleanDNSDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func cleanDNSRecordValue(recordType string, value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(recordType, "CNAME") {
		return cleanDNSDomain(value)
	}
	return value
}

func normalizeManagedDNSRecord(record managedDNSRecord) managedDNSRecord {
	record.PlaneID = strings.TrimSpace(record.PlaneID)
	record.ServiceID = strings.TrimSpace(record.ServiceID)
	record.Host = cleanDNSDomain(record.Host)
	record.RecordType = strings.ToUpper(strings.TrimSpace(record.RecordType))
	record.Value = cleanDNSRecordValue(record.RecordType, record.Value)
	record.Purpose = strings.TrimSpace(record.Purpose)
	return record
}

func validateManagedDNSRecord(record managedDNSRecord) error {
	if record.PlaneID == "" || record.ServiceID == "" || record.Host == "" || record.RecordType == "" || record.Purpose == "" {
		return fmt.Errorf("managed DNS record requires planeID, serviceID, host, recordType and purpose")
	}
	return nil
}

func managedDNSRemark(record managedDNSRecord) string {
	record = normalizeManagedDNSRecord(record)
	return fmt.Sprintf("%s service=%s plane=%s purpose=%s", dnsRecordOwner, record.ServiceID, record.PlaneID, record.Purpose)
}

func dnsRecordOwnedBy(remark string, record managedDNSRecord) bool {
	return strings.TrimSpace(remark) == managedDNSRemark(record)
}

func purposeFromRemark(remark string) string {
	for _, field := range strings.Fields(strings.TrimSpace(remark)) {
		if value, ok := strings.CutPrefix(field, "purpose="); ok {
			return value
		}
	}
	return ""
}

func isDNSPodRecordAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var sdkErr *sdkerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		return strings.EqualFold(strings.TrimSpace(sdkErr.GetCode()), "InvalidParameter.DomainRecordExist")
	}
	return false
}
