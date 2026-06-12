package frontdoor

import (
	"context"
	"fmt"
	"strings"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

const dnsRecordTTL uint64 = 600

type dnspodAPI interface {
	CreateRecordWithContext(context.Context, *dnspod.CreateRecordRequest) (*dnspod.CreateRecordResponse, error)
	DescribeRecordListWithContext(context.Context, *dnspod.DescribeRecordListRequest) (*dnspod.DescribeRecordListResponse, error)
	ModifyRecordWithContext(context.Context, *dnspod.ModifyRecordRequest) (*dnspod.ModifyRecordResponse, error)
	DeleteRecordWithContext(context.Context, *dnspod.DeleteRecordRequest) (*dnspod.DeleteRecordResponse, error)
}

type dnspodClient struct {
	client dnspodAPI
	domain string
}

func newDNSPodClient(cfg cloudplaneconfig.FrontDoorConfig) (*dnspodClient, error) {
	credential := tccommon.NewTokenCredential(
		cfg.DNSPodCredential.SecretID,
		cfg.DNSPodCredential.SecretKey,
		cfg.DNSPodCredential.Token,
	)
	profile := tcprofile.NewClientProfile()
	profile.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	client, err := dnspod.NewClient(credential, "", profile)
	if err != nil {
		return nil, fmt.Errorf("create DNSPod client: %w", err)
	}
	return &dnspodClient{client: client, domain: cleanDomain(cfg.DNSPodDomain)}, nil
}

func (c *dnspodClient) ListRecords(ctx context.Context) ([]DNSRecord, error) {
	limit := uint64(3000)
	errorOnEmpty := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.Limit = &limit
	req.ErrorOnEmpty = &errorOnEmpty

	resp, err := c.client.DescribeRecordListWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	records := make([]DNSRecord, 0, len(resp.Response.RecordList))
	for _, item := range resp.Response.RecordList {
		if record, ok := c.dnsRecord(item); ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func (c *dnspodClient) EnsureRecord(ctx context.Context, host string, recordType string, value string) error {
	host = cleanDomain(host)
	recordType = cleanRecordType(recordType)
	value = cleanRecordValue(&value, recordType)
	subdomain, err := c.subdomain(host)
	if err != nil {
		return err
	}

	records, err := c.recordsForSubdomain(ctx, subdomain, recordType)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Type != recordType {
			continue
		}
		if cleanRecordValue(&record.Value, recordType) == value {
			return nil
		}
		return c.modifyRecord(ctx, record.ID, subdomain, recordType, value)
	}
	return c.createRecord(ctx, subdomain, recordType, value)
}

func (c *dnspodClient) DeleteRecord(ctx context.Context, record DNSRecord) error {
	if record.ID == 0 {
		return nil
	}
	req := dnspod.NewDeleteRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(record.ID)
	_, err := c.client.DeleteRecordWithContext(ctx, req)
	return err
}

func (c *dnspodClient) recordsForSubdomain(ctx context.Context, subdomain string, recordType string) ([]DNSRecord, error) {
	limit := uint64(100)
	errorOnEmpty := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.Subdomain = tccommon.StringPtr(subdomain)
	req.RecordType = tccommon.StringPtr(recordType)
	req.Limit = &limit
	req.ErrorOnEmpty = &errorOnEmpty

	resp, err := c.client.DescribeRecordListWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	records := make([]DNSRecord, 0, len(resp.Response.RecordList))
	for _, item := range resp.Response.RecordList {
		if record, ok := c.dnsRecord(item); ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func (c *dnspodClient) dnsRecord(item *dnspod.RecordListItem) (DNSRecord, bool) {
	if item == nil || item.RecordId == nil || item.Name == nil || item.Type == nil {
		return DNSRecord{}, false
	}
	return DNSRecord{
		ID:        *item.RecordId,
		Subdomain: dnsHost(*item.Name, c.domain),
		Type:      cleanRecordType(*item.Type),
		Value:     cleanRecordValue(item.Value, *item.Type),
	}, true
}

func (c *dnspodClient) createRecord(ctx context.Context, subdomain string, recordType string, value string) error {
	line := "默认"
	req := dnspod.NewCreateRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(value)
	req.TTL = tccommon.Uint64Ptr(dnsRecordTTL)
	_, err := c.client.CreateRecordWithContext(ctx, req)
	return err
}

func (c *dnspodClient) modifyRecord(ctx context.Context, recordID uint64, subdomain string, recordType string, value string) error {
	line := "默认"
	req := dnspod.NewModifyRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(recordID)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(value)
	req.TTL = tccommon.Uint64Ptr(dnsRecordTTL)
	_, err := c.client.ModifyRecordWithContext(ctx, req)
	return err
}

func (c *dnspodClient) subdomain(host string) (string, error) {
	host = cleanDomain(host)
	if host == c.domain {
		return "@", nil
	}
	suffix := "." + c.domain
	if !strings.HasSuffix(host, suffix) {
		return "", fmt.Errorf("host %q is outside DNSPod domain %q", host, c.domain)
	}
	return strings.TrimSuffix(host, suffix), nil
}

func dnsHost(name string, root string) string {
	name = strings.Trim(strings.TrimSpace(name), ".")
	root = cleanDomain(root)
	if name == "" || name == "@" {
		return root
	}
	return cleanDomain(name + "." + root)
}

func trimCNAMEValue(value *string) string {
	if value == nil {
		return ""
	}
	return trimCNAME(*value)
}

func trimCNAME(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func cleanRecordType(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func cleanRecordValue(value *string, recordType string) string {
	if value == nil {
		return ""
	}
	if cleanRecordType(recordType) == "CNAME" {
		return trimCNAME(*value)
	}
	return strings.TrimSpace(*value)
}
