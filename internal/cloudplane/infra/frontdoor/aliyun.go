package frontdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	cdn20180510 "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/aliyun/credentials-go/credentials"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

type aliyunCDNAPI interface {
	AddCdnDomain(*cdn20180510.AddCdnDomainRequest) (*cdn20180510.AddCdnDomainResponse, error)
	BatchSetCdnDomainConfig(*cdn20180510.BatchSetCdnDomainConfigRequest) (*cdn20180510.BatchSetCdnDomainConfigResponse, error)
	DescribeUserDomains(*cdn20180510.DescribeUserDomainsRequest) (*cdn20180510.DescribeUserDomainsResponse, error)
	StopCdnDomain(*cdn20180510.StopCdnDomainRequest) (*cdn20180510.StopCdnDomainResponse, error)
	DeleteCdnDomain(*cdn20180510.DeleteCdnDomainRequest) (*cdn20180510.DeleteCdnDomainResponse, error)
}

type aliyunCDNClient struct {
	client aliyunCDNAPI
	origin string
}

func newAliyunCDNClient(cfg cloudplaneconfig.Config) (*aliyunCDNClient, error) {
	credential, err := credentials.NewCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create aliyun credential: %w", err)
	}
	endpoint := "cdn.aliyuncs.com"
	client, err := cdn20180510.NewClient(&openapi.Config{
		Endpoint:   &endpoint,
		Credential: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("create aliyun CDN client: %w", err)
	}
	return &aliyunCDNClient{client: client, origin: cfg.Ingress.PublicOrigin}, nil
}

func (c *aliyunCDNClient) ListDomains(_ context.Context, baseDomain string) ([]CDNDomain, error) {
	match := "suf_match"
	pageNumber := int32(1)
	pageSize := int32(500)
	resp, err := c.client.DescribeUserDomains(&cdn20180510.DescribeUserDomainsRequest{
		DomainName:       &baseDomain,
		DomainSearchType: &match,
		PageNumber:       &pageNumber,
		PageSize:         &pageSize,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil || resp.Body.Domains == nil {
		return nil, nil
	}
	domains := make([]CDNDomain, 0, len(resp.Body.Domains.PageData))
	for _, item := range resp.Body.Domains.PageData {
		if item == nil || item.DomainName == nil {
			continue
		}
		domains = append(domains, CDNDomain{Host: cleanDomain(*item.DomainName), CNAME: trimCNAMEValue(item.Cname)})
	}
	return domains, nil
}

func (c *aliyunCDNClient) EnsureDomain(ctx context.Context, host string) (string, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(host)
	if err != nil {
		return "", err
	}
	if domain.Host == "" {
		if err := c.addDomain(host); err != nil {
			return "", err
		}
		if err := c.setOriginHost(host); err != nil {
			return "", err
		}
		domain, err = c.getDomain(host)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(domain.CNAME) == "" {
		return "", fmt.Errorf("domain exists but CNAME is empty")
	}
	return domain.CNAME, nil
}

func (c *aliyunCDNClient) DeleteDomain(_ context.Context, host string) error {
	host = cleanDomain(host)
	if host == "" {
		return nil
	}
	if _, err := c.client.StopCdnDomain((&cdn20180510.StopCdnDomainRequest{}).SetDomainName(host)); err != nil && !isAliyunNotFound(err) {
		return err
	}
	if _, err := c.client.DeleteCdnDomain((&cdn20180510.DeleteCdnDomainRequest{}).SetDomainName(host)); err != nil && !isAliyunNotFound(err) {
		return err
	}
	return nil
}

func (c *aliyunCDNClient) getDomain(host string) (CDNDomain, error) {
	match := "full_match"
	resp, err := c.client.DescribeUserDomains(&cdn20180510.DescribeUserDomainsRequest{
		DomainName:       &host,
		DomainSearchType: &match,
	})
	if err != nil {
		return CDNDomain{}, err
	}
	if resp == nil || resp.Body == nil || resp.Body.Domains == nil {
		return CDNDomain{}, nil
	}
	for _, item := range resp.Body.Domains.PageData {
		if item == nil || item.DomainName == nil || cleanDomain(*item.DomainName) != host {
			continue
		}
		return CDNDomain{Host: host, CNAME: trimCNAMEValue(item.Cname)}, nil
	}
	return CDNDomain{}, nil
}

func (c *aliyunCDNClient) addDomain(host string) error {
	sources, err := json.Marshal([]map[string]any{{
		"type":     originType(c.origin),
		"content":  c.origin,
		"port":     80,
		"priority": "20",
		"weight":   "15",
	}})
	if err != nil {
		return err
	}
	cdnType := "web"
	scope := "domestic"
	sourceJSON := string(sources)
	_, err = c.client.AddCdnDomain(&cdn20180510.AddCdnDomainRequest{
		DomainName: &host,
		CdnType:    &cdnType,
		Scope:      &scope,
		Sources:    &sourceJSON,
	})
	return err
}

func (c *aliyunCDNClient) setOriginHost(host string) error {
	functions, err := json.Marshal([]map[string]any{{
		"functionName": "set_req_host_header",
		"functionArgs": []map[string]string{{
			"argName":  "domain_name",
			"argValue": host,
		}},
	}})
	if err != nil {
		return err
	}
	functionJSON := string(functions)
	_, err = c.client.BatchSetCdnDomainConfig(&cdn20180510.BatchSetCdnDomainConfigRequest{
		DomainNames: &host,
		Functions:   &functionJSON,
	})
	return err
}

func originType(origin string) string {
	if net.ParseIP(strings.TrimSpace(origin)) != nil {
		return "ipaddr"
	}
	return "domain"
}

func isAliyunNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "notfound") || strings.Contains(message, "not exist") || strings.Contains(message, "not exists")
}
