package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	cdn "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdn/v20180606"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

type tencentCDNAPI interface {
	AddCdnDomainWithContext(context.Context, *cdn.AddCdnDomainRequest) (*cdn.AddCdnDomainResponse, error)
	CreateVerifyRecordWithContext(context.Context, *cdn.CreateVerifyRecordRequest) (*cdn.CreateVerifyRecordResponse, error)
	DescribeDomainsWithContext(context.Context, *cdn.DescribeDomainsRequest) (*cdn.DescribeDomainsResponse, error)
	StopCdnDomainWithContext(context.Context, *cdn.StopCdnDomainRequest) (*cdn.StopCdnDomainResponse, error)
	DeleteCdnDomainWithContext(context.Context, *cdn.DeleteCdnDomainRequest) (*cdn.DeleteCdnDomainResponse, error)
	VerifyDomainRecordWithContext(context.Context, *cdn.VerifyDomainRecordRequest) (*cdn.VerifyDomainRecordResponse, error)
}

type tencentCDNClient struct {
	client        tencentCDNAPI
	origin        string
	dnsRootDomain string
}

func newTencentCDNClient(cfg cloudplaneconfig.Config) (*tencentCDNClient, error) {
	credential := tccommon.NewTokenCredential(
		cfg.Infrastructure.TencentCredential.SecretID,
		cfg.Infrastructure.TencentCredential.SecretKey,
		cfg.Infrastructure.TencentCredential.Token,
	)
	profile := tcprofile.NewClientProfile()
	profile.HttpProfile.Endpoint = "cdn.tencentcloudapi.com"
	client, err := cdn.NewClient(credential, "", profile)
	if err != nil {
		return nil, fmt.Errorf("create Tencent CDN client: %w", err)
	}
	return &tencentCDNClient{client: client, origin: cfg.Ingress.PublicOrigin, dnsRootDomain: rootDomain(cfg.Ingress.BaseDomain)}, nil
}

func (c *tencentCDNClient) PrepareDomain(ctx context.Context, host string) (*DNSRecord, error) {
	host = cleanDomain(host)
	if host == "" {
		return nil, nil
	}
	domain, err := c.getDomain(ctx, host)
	if err != nil {
		return nil, err
	}
	if domain.Exists {
		return nil, nil
	}
	recordReq := cdn.NewCreateVerifyRecordRequest()
	recordReq.Domain = tccommon.StringPtr(host)
	recordResp, err := c.client.CreateVerifyRecordWithContext(ctx, recordReq)
	if err != nil {
		return nil, err
	}
	if recordResp == nil || recordResp.Response == nil || recordResp.Response.SubDomain == nil || recordResp.Response.Record == nil || recordResp.Response.RecordType == nil {
		return nil, fmt.Errorf("tencent CDN verify record response is incomplete")
	}
	verifyHost := cleanDomain(*recordResp.Response.SubDomain + "." + c.dnsRootDomain)
	verifyRecord := &DNSRecord{
		Subdomain: verifyHost,
		Type:      *recordResp.Response.RecordType,
		Value:     *recordResp.Response.Record,
	}
	verifyType := "dns"
	verifyReq := cdn.NewVerifyDomainRecordRequest()
	verifyReq.Domain = tccommon.StringPtr(host)
	verifyReq.VerifyType = tccommon.StringPtr(verifyType)
	verifyResp, err := c.client.VerifyDomainRecordWithContext(ctx, verifyReq)
	if err != nil {
		if isTencentVerifyPending(err) {
			return verifyRecord, errDomainVerificationPending
		}
		return verifyRecord, err
	}
	if verifyResp == nil || verifyResp.Response == nil || verifyResp.Response.Result == nil || !*verifyResp.Response.Result {
		return verifyRecord, errDomainVerificationPending
	}
	return verifyRecord, nil
}

func rootDomain(value string) string {
	parts := strings.Split(cleanDomain(value), ".")
	if len(parts) < 2 {
		return cleanDomain(value)
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func (c *tencentCDNClient) EnsureDomain(ctx context.Context, host string) (string, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(ctx, host)
	if err != nil {
		return "", err
	}
	if !domain.Exists {
		if err := c.addDomain(ctx, host); err != nil {
			if isTencentPending(err) {
				return "", nil
			}
			return "", err
		}
		return "", nil
	}
	if strings.TrimSpace(domain.CNAME) == "" {
		return "", nil
	}
	return domain.CNAME, nil
}

func (c *tencentCDNClient) DeleteDomain(ctx context.Context, host string) error {
	host = cleanDomain(host)
	if host == "" {
		return nil
	}
	stopReq := cdn.NewStopCdnDomainRequest()
	stopReq.Domain = tccommon.StringPtr(host)
	if _, err := c.client.StopCdnDomainWithContext(ctx, stopReq); err != nil && !isTencentNotFound(err) {
		return err
	}
	deleteReq := cdn.NewDeleteCdnDomainRequest()
	deleteReq.Domain = tccommon.StringPtr(host)
	if _, err := c.client.DeleteCdnDomainWithContext(ctx, deleteReq); err != nil && !isTencentNotFound(err) {
		return err
	}
	return nil
}

func (c *tencentCDNClient) getDomain(ctx context.Context, host string) (cdnDomain, error) {
	req := cdn.NewDescribeDomainsRequest()
	req.Offset = tccommon.Int64Ptr(0)
	req.Limit = tccommon.Int64Ptr(10)
	req.Filters = []*cdn.DomainFilter{{
		Name:  tccommon.StringPtr("domain"),
		Value: []*string{tccommon.StringPtr(host)},
	}}
	resp, err := c.client.DescribeDomainsWithContext(ctx, req)
	if err != nil {
		return cdnDomain{}, err
	}
	if resp.Response == nil {
		return cdnDomain{}, nil
	}
	for _, item := range resp.Response.Domains {
		if item == nil || item.Domain == nil || cleanDomain(*item.Domain) != host {
			continue
		}
		return cdnDomain{Exists: true, CNAME: trimCNAMEValue(item.Cname)}, nil
	}
	return cdnDomain{}, nil
}

func (c *tencentCDNClient) addDomain(ctx context.Context, host string) error {
	req := cdn.NewAddCdnDomainRequest()
	req.Domain = tccommon.StringPtr(host)
	req.ServiceType = tccommon.StringPtr("web")
	req.Origin = &cdn.Origin{
		Origins:            []*string{tccommon.StringPtr(c.origin)},
		OriginType:         tccommon.StringPtr(tencentOriginType(c.origin)),
		ServerName:         tccommon.StringPtr(host),
		OriginPullProtocol: tccommon.StringPtr("http"),
	}
	_, err := c.client.AddCdnDomainWithContext(ctx, req)
	return err
}

func tencentOriginType(origin string) string {
	if net.ParseIP(strings.TrimSpace(origin)) != nil {
		return "ip"
	}
	return "domain"
}

func isTencentNotFound(err error) bool {
	if err == nil {
		return false
	}
	var sdkErr *sdkerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := strings.ToLower(sdkErr.GetCode())
		return strings.Contains(code, "notfound") || strings.Contains(code, "notexists")
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "not exist")
}

func isTencentPending(err error) bool {
	if err == nil {
		return false
	}
	var sdkErr *sdkerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := strings.ToLower(sdkErr.GetCode())
		return strings.Contains(code, "deploying") || strings.Contains(code, "processing") || strings.Contains(code, "busy")
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deploying") || strings.Contains(message, "processing") || strings.Contains(message, "busy")
}

func isTencentVerifyPending(err error) bool {
	if err == nil {
		return false
	}
	var sdkErr *sdkerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := strings.ToLower(sdkErr.GetCode())
		return strings.Contains(code, "txtrecordvaluenotmatch") || strings.Contains(code, "operationtoooften")
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "txt record") || strings.Contains(message, "too often")
}
