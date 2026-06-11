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
	req.RecordType = tccommon.StringPtr("CNAME")
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
		if item == nil || item.RecordId == nil || item.Name == nil || item.Type == nil {
			continue
		}
		records = append(records, DNSRecord{
			ID:        *item.RecordId,
			Subdomain: dnsHost(*item.Name, c.domain),
			Type:      strings.ToUpper(strings.TrimSpace(*item.Type)),
			Value:     trimCNAMEValue(item.Value),
		})
	}
	return records, nil
}

func (c *dnspodClient) EnsureCNAME(ctx context.Context, host string, cname string) error {
	host = cleanDomain(host)
	cname = trimCNAME(cname)
	subdomain, err := c.subdomain(host)
	if err != nil {
		return err
	}

	records, err := c.recordsForSubdomain(ctx, subdomain)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Type != "CNAME" {
			continue
		}
		if trimCNAME(record.Value) == cname {
			return nil
		}
		return c.modifyRecord(ctx, record.ID, subdomain, cname)
	}
	return c.createRecord(ctx, subdomain, cname)
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

func (c *dnspodClient) recordsForSubdomain(ctx context.Context, subdomain string) ([]DNSRecord, error) {
	limit := uint64(100)
	errorOnEmpty := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.Subdomain = tccommon.StringPtr(subdomain)
	req.RecordType = tccommon.StringPtr("CNAME")
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
		if item == nil || item.RecordId == nil || item.Name == nil || item.Type == nil {
			continue
		}
		records = append(records, DNSRecord{
			ID:        *item.RecordId,
			Subdomain: dnsHost(*item.Name, c.domain),
			Type:      strings.ToUpper(strings.TrimSpace(*item.Type)),
			Value:     trimCNAMEValue(item.Value),
		})
	}
	return records, nil
}

func (c *dnspodClient) createRecord(ctx context.Context, subdomain string, cname string) error {
	line := "默认"
	recordType := "CNAME"
	req := dnspod.NewCreateRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(cname)
	req.TTL = tccommon.Uint64Ptr(dnsRecordTTL)
	_, err := c.client.CreateRecordWithContext(ctx, req)
	return err
}

func (c *dnspodClient) modifyRecord(ctx context.Context, recordID uint64, subdomain string, cname string) error {
	line := "默认"
	recordType := "CNAME"
	req := dnspod.NewModifyRecordRequest()
	req.Domain = tccommon.StringPtr(c.domain)
	req.RecordId = tccommon.Uint64Ptr(recordID)
	req.SubDomain = tccommon.StringPtr(subdomain)
	req.RecordType = &recordType
	req.RecordLine = &line
	req.Value = tccommon.StringPtr(cname)
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
