package frontdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	cdn20180510 "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/aliyun/credentials-go/credentials"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

type aliyunCDNAPI interface {
	AddCdnDomainWithOptions(*cdn20180510.AddCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.AddCdnDomainResponse, error)
	BatchSetCdnDomainConfigWithOptions(*cdn20180510.BatchSetCdnDomainConfigRequest, *util.RuntimeOptions) (*cdn20180510.BatchSetCdnDomainConfigResponse, error)
	DescribeUserDomainsWithOptions(*cdn20180510.DescribeUserDomainsRequest, *util.RuntimeOptions) (*cdn20180510.DescribeUserDomainsResponse, error)
	StopCdnDomainWithOptions(*cdn20180510.StopCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.StopCdnDomainResponse, error)
	DeleteCdnDomainWithOptions(*cdn20180510.DeleteCdnDomainRequest, *util.RuntimeOptions) (*cdn20180510.DeleteCdnDomainResponse, error)
	VerifyDomainOwnerWithOptions(*cdn20180510.VerifyDomainOwnerRequest, *util.RuntimeOptions) (*cdn20180510.VerifyDomainOwnerResponse, error)
}

type aliyunRawAPI interface {
	CallApiWithCtx(context.Context, *openapi.Params, *openapi.OpenApiRequest, *util.RuntimeOptions) (map[string]interface{}, error)
}

type aliyunCDNClient struct {
	client    aliyunCDNAPI
	rawClient aliyunRawAPI
	origin    string
}

const (
	aliyunCDNConnectTimeout = 5 * time.Second
	aliyunCDNReadTimeout    = 15 * time.Second
)

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
		client:    client,
		rawClient: client,
		origin:    cfg.Ingress.PublicOrigin,
	}, nil
}

func (c *aliyunCDNClient) PrepareDomain(ctx context.Context, host string) (*DNSRecord, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(host)
	if err != nil {
		return nil, err
	}
	if domain.Exists {
		return nil, nil
	}
	verify, err := c.domainVerifyData(ctx, host)
	if err != nil {
		return nil, err
	}
	verifyRecord := &DNSRecord{
		Subdomain: verify.host(),
		Type:      "TXT",
		Value:     verify.VerifyCode,
	}
	verifyType := "dnsCheck"
	_, err = c.client.VerifyDomainOwnerWithOptions(&cdn20180510.VerifyDomainOwnerRequest{
		DomainName: &host,
		VerifyType: &verifyType,
	}, aliyunCDNRuntimeOptions())
	if isAliyunPending(err) {
		return verifyRecord, errDomainVerificationPending
	}
	return verifyRecord, err
}

func (c *aliyunCDNClient) EnsureDomain(ctx context.Context, host string) (string, error) {
	host = cleanDomain(host)
	domain, err := c.getDomain(host)
	if err != nil {
		return "", err
	}
	if !domain.Exists {
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
	if err := c.setOriginHost(host); err != nil {
		if isAliyunPending(err) {
			return "", nil
		}
		return "", err
	}
	return domain.CNAME, nil
}

func (c *aliyunCDNClient) DeleteDomain(_ context.Context, host string) error {
	host = cleanDomain(host)
	if host == "" {
		return nil
	}
	if _, err := c.client.StopCdnDomainWithOptions((&cdn20180510.StopCdnDomainRequest{}).SetDomainName(host), aliyunCDNRuntimeOptions()); err != nil && !isAliyunNotFound(err) {
		return err
	}
	if _, err := c.client.DeleteCdnDomainWithOptions((&cdn20180510.DeleteCdnDomainRequest{}).SetDomainName(host), aliyunCDNRuntimeOptions()); err != nil && !isAliyunNotFound(err) {
		return err
	}
	return nil
}

func (c *aliyunCDNClient) getDomain(host string) (cdnDomain, error) {
	match := "full_match"
	resp, err := c.client.DescribeUserDomainsWithOptions(&cdn20180510.DescribeUserDomainsRequest{
		DomainName:       &host,
		DomainSearchType: &match,
	}, aliyunCDNRuntimeOptions())
	if err != nil {
		return cdnDomain{}, err
	}
	if resp == nil || resp.Body == nil || resp.Body.Domains == nil {
		return cdnDomain{}, nil
	}
	for _, item := range resp.Body.Domains.PageData {
		if item == nil || item.DomainName == nil || cleanDomain(*item.DomainName) != host {
			continue
		}
		return cdnDomain{Exists: true, CNAME: cleanDomainPointer(item.Cname)}, nil
	}
	return cdnDomain{}, nil
}

func (c *aliyunCDNClient) domainVerifyData(ctx context.Context, host string) (aliyunDomainVerifyData, error) {
	if c.rawClient == nil {
		return aliyunDomainVerifyData{}, fmt.Errorf("aliyun raw API client is nil")
	}
	result, err := c.rawClient.CallApiWithCtx(ctx, (&openapi.Params{}).
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
	}, aliyunCDNRuntimeOptions())
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
	_, err = c.client.AddCdnDomainWithOptions(&cdn20180510.AddCdnDomainRequest{
		DomainName: &host,
		CdnType:    &cdnType,
		Scope:      &scope,
		Sources:    &sourceJSON,
	}, aliyunCDNRuntimeOptions())
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
	_, err = c.client.BatchSetCdnDomainConfigWithOptions(&cdn20180510.BatchSetCdnDomainConfigRequest{
		DomainNames: &host,
		Functions:   &functionJSON,
	}, aliyunCDNRuntimeOptions())
	return err
}

func aliyunCDNRuntimeOptions() *util.RuntimeOptions {
	connectTimeout := int(aliyunCDNConnectTimeout / time.Millisecond)
	readTimeout := int(aliyunCDNReadTimeout / time.Millisecond)
	return &util.RuntimeOptions{
		ConnectTimeout: &connectTimeout,
		ReadTimeout:    &readTimeout,
	}
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
