package frontdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	cdn20180510 "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/aliyun/credentials-go/credentials"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

type aliyunCDNAPI interface {
	AddCdnDomain(*cdn20180510.AddCdnDomainRequest) (*cdn20180510.AddCdnDomainResponse, error)
	BatchSetCdnDomainConfig(*cdn20180510.BatchSetCdnDomainConfigRequest) (*cdn20180510.BatchSetCdnDomainConfigResponse, error)
	DescribeUserDomains(*cdn20180510.DescribeUserDomainsRequest) (*cdn20180510.DescribeUserDomainsResponse, error)
	StopCdnDomain(*cdn20180510.StopCdnDomainRequest) (*cdn20180510.StopCdnDomainResponse, error)
	DeleteCdnDomain(*cdn20180510.DeleteCdnDomainRequest) (*cdn20180510.DeleteCdnDomainResponse, error)
	VerifyDomainOwner(*cdn20180510.VerifyDomainOwnerRequest) (*cdn20180510.VerifyDomainOwnerResponse, error)
}

type aliyunRawAPI interface {
	CallApi(*openapi.Params, *openapi.OpenApiRequest, *util.RuntimeOptions) (map[string]interface{}, error)
}

type aliyunCDNClient struct {
	client               aliyunCDNAPI
	rawClient            aliyunRawAPI
	origin               string
	originHostConfigured map[string]struct{}
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
	return &aliyunCDNClient{
		client:               client,
		rawClient:            client,
		origin:               cfg.Ingress.PublicOrigin,
		originHostConfigured: map[string]struct{}{},
	}, nil
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

func (c *aliyunCDNClient) PrepareDomain(ctx context.Context, host string, dns dnsClient) error {
	host = cleanDomain(host)
	domain, err := c.getDomain(host)
	if err != nil {
		return err
	}
	if domain.Host != "" {
		return nil
	}
	verify, err := c.domainVerifyData(host)
	if err != nil {
		return err
	}
	if err := dns.EnsureRecord(ctx, verify.host(), "TXT", verify.VerifyCode); err != nil {
		return err
	}
	verifyType := "dnsCheck"
	_, err = c.client.VerifyDomainOwner(&cdn20180510.VerifyDomainOwnerRequest{
		DomainName: &host,
		VerifyType: &verifyType,
	})
	if isAliyunPending(err) {
		return nil
	}
	return err
}

func (c *aliyunCDNClient) EnsureDomain(ctx context.Context, host string) (string, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(host)
	if err != nil {
		return "", err
	}
	if domain.Host == "" {
		if err := c.addDomain(host); err != nil {
			if isAliyunPending(err) {
				return "", nil
			}
			return "", err
		}
		return "", nil
	}
	if strings.TrimSpace(domain.CNAME) == "" {
		return "", nil
	}
	if c.originHostConfigured == nil {
		c.originHostConfigured = map[string]struct{}{}
	}
	if _, ok := c.originHostConfigured[host]; !ok {
		if err := c.setOriginHost(host); err != nil {
			if isAliyunPending(err) {
				return "", nil
			}
			return "", err
		}
		c.originHostConfigured[host] = struct{}{}
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

func (c *aliyunCDNClient) domainVerifyData(host string) (aliyunDomainVerifyData, error) {
	if c.rawClient == nil {
		return aliyunDomainVerifyData{}, fmt.Errorf("aliyun raw API client is nil")
	}
	result, err := c.rawClient.CallApi((&openapi.Params{}).
		SetAction("DescribeDomainVerifyData").
		SetVersion("2018-05-10").
		SetProtocol("HTTPS").
		SetPathname("/").
		SetMethod("POST").
		SetAuthType("AK").
		SetStyle("RPC").
		SetReqBodyType("formData").
		SetBodyType("json"), &openapi.OpenApiRequest{
		Query: openapiutil.Query(map[string]interface{}{"DomainName": host}),
	}, &util.RuntimeOptions{})
	if err != nil {
		return aliyunDomainVerifyData{}, err
	}
	body, _ := result["body"].(map[string]interface{})
	content, _ := body["Content"].(map[string]interface{})
	if len(content) == 0 {
		return aliyunDomainVerifyData{}, fmt.Errorf("domain verify data is empty")
	}
	var verify aliyunDomainVerifyData
	payload, err := json.Marshal(content)
	if err != nil {
		return aliyunDomainVerifyData{}, fmt.Errorf("encode domain verify data: %w", err)
	}
	if err := json.Unmarshal(payload, &verify); err != nil {
		return aliyunDomainVerifyData{}, fmt.Errorf("parse domain verify data: %w", err)
	}
	verify.RootDomain = cleanDomain(verify.RootDomain)
	verify.VerifyKey = strings.Trim(strings.TrimSpace(verify.VerifyKey), ".")
	verify.VerifyCode = strings.TrimSpace(verify.VerifyCode)
	if verify.RootDomain == "" || verify.VerifyKey == "" || verify.VerifyCode == "" {
		return aliyunDomainVerifyData{}, fmt.Errorf("domain verify data is incomplete")
	}
	return verify, nil
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

type aliyunDomainVerifyData struct {
	RootDomain string `json:"RootDomain"`
	VerifyKey  string `json:"verifyKey"`
	VerifyCode string `json:"verifyCode"`
}

func (v aliyunDomainVerifyData) host() string {
	return cleanDomain(v.VerifyKey + "." + v.RootDomain)
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

func isAliyunPending(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "domainownerverifyfail") ||
		strings.Contains(message, "servicebusy") ||
		strings.Contains(message, "configuring")
}
