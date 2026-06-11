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
	DescribeDomainsWithContext(context.Context, *cdn.DescribeDomainsRequest) (*cdn.DescribeDomainsResponse, error)
	StopCdnDomainWithContext(context.Context, *cdn.StopCdnDomainRequest) (*cdn.StopCdnDomainResponse, error)
	DeleteCdnDomainWithContext(context.Context, *cdn.DeleteCdnDomainRequest) (*cdn.DeleteCdnDomainResponse, error)
}

type tencentCDNClient struct {
	client tencentCDNAPI
	origin string
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
	return &tencentCDNClient{client: client, origin: cfg.Ingress.PublicOrigin}, nil
}

func (c *tencentCDNClient) ListDomains(ctx context.Context, baseDomain string) ([]CDNDomain, error) {
	req := cdn.NewDescribeDomainsRequest()
	req.Offset = tccommon.Int64Ptr(0)
	req.Limit = tccommon.Int64Ptr(1000)
	req.Filters = []*cdn.DomainFilter{{
		Name:  tccommon.StringPtr("domain"),
		Value: []*string{tccommon.StringPtr(baseDomain)},
		Fuzzy: tccommon.BoolPtr(true),
	}}
	resp, err := c.client.DescribeDomainsWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	domains := make([]CDNDomain, 0, len(resp.Response.Domains))
	for _, item := range resp.Response.Domains {
		if item == nil || item.Domain == nil {
			continue
		}
		domains = append(domains, CDNDomain{Host: cleanDomain(*item.Domain), CNAME: trimCNAMEValue(item.Cname)})
	}
	return domains, nil
}

func (c *tencentCDNClient) EnsureDomain(ctx context.Context, host string) (string, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(ctx, host)
	if err != nil {
		return "", err
	}
	if domain.Host == "" {
		if err := c.addDomain(ctx, host); err != nil {
			return "", err
		}
		domain, err = c.getDomain(ctx, host)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(domain.CNAME) == "" {
		return "", fmt.Errorf("domain exists but CNAME is empty")
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

func (c *tencentCDNClient) getDomain(ctx context.Context, host string) (CDNDomain, error) {
	req := cdn.NewDescribeDomainsRequest()
	req.Offset = tccommon.Int64Ptr(0)
	req.Limit = tccommon.Int64Ptr(10)
	req.Filters = []*cdn.DomainFilter{{
		Name:  tccommon.StringPtr("domain"),
		Value: []*string{tccommon.StringPtr(host)},
	}}
	resp, err := c.client.DescribeDomainsWithContext(ctx, req)
	if err != nil {
		return CDNDomain{}, err
	}
	if resp.Response == nil {
		return CDNDomain{}, nil
	}
	for _, item := range resp.Response.Domains {
		if item == nil || item.Domain == nil || cleanDomain(*item.Domain) != host {
			continue
		}
		return CDNDomain{Host: host, CNAME: trimCNAMEValue(item.Cname)}, nil
	}
	return CDNDomain{}, nil
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
