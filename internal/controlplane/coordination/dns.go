package coordination

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/config"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

const (
	dnsRecordTTL         uint64 = 600
	dnsPodRequestTimeout        = 15 * time.Second
)

type dnsClient interface {
	EnsureRecord(context.Context, string, string, string) error
	DeleteRecord(context.Context, string, string, string) error
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
	credential := tccommon.NewTokenCredential(cfg.SecretID, cfg.SecretKey, cfg.Token)
	profile := tcprofile.NewClientProfile()
	profile.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	profile.HttpProfile.ReqTimeout = int(dnsPodRequestTimeout / time.Second)
	client, err := dnspod.NewClient(credential, "", profile)
	if err != nil {
		return nil, fmt.Errorf("create DNSPod client: %w", err)
	}
	return &dnsPodClient{client: client, domain: cleanDNSDomain(cfg.Domain)}, nil
}

func (c *dnsPodClient) EnsureRecord(ctx context.Context, host string, recordType string, value string) error {
	host = cleanDNSDomain(host)
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	value = cleanDNSRecordValue(recordType, value)
	subdomain, err := c.subdomain(host)
	if err != nil {
		return err
	}

	records, err := c.recordsForSubdomain(ctx, subdomain, recordType)
	if err != nil {
		return err
	}
	for _, record := range records {
		if cleanDNSRecordValue(recordType, record.value) == value {
			return nil
		}
		return c.modifyRecord(ctx, record.id, subdomain, recordType, value)
	}
	return c.createRecord(ctx, subdomain, recordType, value)
}

func (c *dnsPodClient) DeleteRecord(ctx context.Context, host string, recordType string, value string) error {
	host = cleanDNSDomain(host)
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	value = cleanDNSRecordValue(recordType, value)
	subdomain, err := c.subdomain(host)
	if err != nil {
		return err
	}
	records, err := c.recordsForSubdomain(ctx, subdomain, recordType)
	if err != nil {
		return err
	}
	for _, record := range records {
		if value != "" && cleanDNSRecordValue(recordType, record.value) != value {
			continue
		}
		if err := c.deleteRecord(ctx, record.id); err != nil {
			return err
		}
	}
	return nil
}

type dnsRecord struct {
	id    uint64
	value string
}

func NewDNSClient(cfg config.DNSPodConfig) (dnsClient, error) {
	return newDNSPodClient(cfg)
}

func (c *dnsPodClient) recordsForSubdomain(ctx context.Context, subdomain string, recordType string) ([]dnsRecord, error) {
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
	out := make([]dnsRecord, 0, len(resp.Response.RecordList))
	for _, item := range resp.Response.RecordList {
		if item == nil || item.RecordId == nil || item.Value == nil {
			continue
		}
		out = append(out, dnsRecord{id: *item.RecordId, value: *item.Value})
	}
	return out, nil
}

func (c *dnsPodClient) createRecord(ctx context.Context, subdomain string, recordType string, value string) error {
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

func (c *dnsPodClient) modifyRecord(ctx context.Context, recordID uint64, subdomain string, recordType string, value string) error {
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

func (c *dnsPodClient) deleteRecord(ctx context.Context, recordID uint64) error {
	req := dnspod.NewDeleteRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(recordID)
	_, err := c.client.DeleteRecordWithContext(ctx, req)
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
