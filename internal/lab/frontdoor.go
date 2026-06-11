package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	alidns "github.com/alibabacloud-go/alidns-20150109/v4/client"
	cdnali "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	tea "github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
	cdntencent "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cdn/v20180606"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

type frontDoorConfig struct {
	Provider   string
	BaseDomain string
	OriginHost string
}

func (r *Runner) ensureFrontDoor(ctx context.Context, out TerraformOutput) error {
	cfg, err := r.frontDoorConfig(out)
	if err != nil {
		return err
	}
	if cfg.BaseDomain == "" {
		return nil
	}
	driver, err := r.frontDoorDriver(cfg.Provider)
	if err != nil {
		return err
	}
	if err := driver.ensure(ctx, cfg); err != nil {
		return err
	}
	fmt.Printf("ingress front door ready: %s -> %s\n", wildcardDomain(cfg.BaseDomain), cfg.OriginHost)
	return nil
}

func (r *Runner) deleteFrontDoor(ctx context.Context, out TerraformOutput) error {
	cfg, err := r.frontDoorConfig(out)
	if err != nil {
		return err
	}
	if cfg.BaseDomain == "" {
		return nil
	}
	driver, err := r.frontDoorDriver(cfg.Provider)
	if err != nil {
		return err
	}
	if err := driver.delete(ctx, cfg); err != nil {
		return err
	}
	fmt.Printf("ingress front door deleted: %s\n", wildcardDomain(cfg.BaseDomain))
	return nil
}

func (r *Runner) frontDoorConfig(out TerraformOutput) (frontDoorConfig, error) {
	baseDomain := strings.Trim(strings.TrimSpace(r.cfg.Install.IngressBaseDomain), ".")
	if baseDomain == "" {
		return frontDoorConfig{}, nil
	}
	originHost := strings.Trim(strings.TrimSpace(r.cfg.Install.IngressOriginHost), ".")
	if originHost == "" {
		return frontDoorConfig{}, fmt.Errorf("install.ingressOriginHost is required when install.ingressBaseDomain is set")
	}
	provider := out.ProviderName()
	if provider == "" {
		return frontDoorConfig{}, fmt.Errorf("terraform provider output is empty")
	}
	return frontDoorConfig{Provider: provider, BaseDomain: baseDomain, OriginHost: originHost}, nil
}

type frontDoorDriver interface {
	ensure(context.Context, frontDoorConfig) error
	delete(context.Context, frontDoorConfig) error
}

func (r *Runner) frontDoorDriver(provider string) (frontDoorDriver, error) {
	switch provider {
	case "aliyun":
		return newAliyunFrontDoor()
	case "tencent":
		return r.newTencentFrontDoor()
	default:
		return nil, fmt.Errorf("front door provider %q is not implemented", provider)
	}
}

type tencentFrontDoor struct {
	cdn *cdntencent.Client
	dns *dnspod.Client
}

func (r *Runner) newTencentFrontDoor() (*tencentFrontDoor, error) {
	credential, err := readTencentCredentialFile(r.cfg.Provider.TencentCredentialFile)
	if err != nil {
		return nil, err
	}
	if credential.SecretID == "" || credential.SecretKey == "" {
		return nil, fmt.Errorf("provider.tencentCredentialFile must contain secretId and secretKey")
	}
	tcCredential := tccommon.NewTokenCredential(credential.SecretID, credential.SecretKey, credential.Token)

	cdnProfile := tcprofile.NewClientProfile()
	cdnProfile.HttpProfile.Endpoint = "cdn.tencentcloudapi.com"
	cdnClient, err := cdntencent.NewClient(tcCredential, "", cdnProfile)
	if err != nil {
		return nil, fmt.Errorf("create Tencent CDN client: %w", err)
	}
	dnsProfile := tcprofile.NewClientProfile()
	dnsProfile.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	dnsClient, err := dnspod.NewClient(tcCredential, "", dnsProfile)
	if err != nil {
		return nil, fmt.Errorf("create DNSPod client: %w", err)
	}
	return &tencentFrontDoor{cdn: cdnClient, dns: dnsClient}, nil
}

func (d *tencentFrontDoor) ensure(ctx context.Context, cfg frontDoorConfig) error {
	domain := wildcardDomain(cfg.BaseDomain)
	cname, err := d.ensureTencentCDNDomain(ctx, domain, cfg.OriginHost)
	if err != nil {
		return err
	}
	rootDomain, subdomain, err := splitWildcardDomain(domain)
	if err != nil {
		return err
	}
	_, err = d.ensureTencentDNSRecord(ctx, rootDomain, subdomain, "CNAME", cname)
	return err
}

func (d *tencentFrontDoor) delete(ctx context.Context, cfg frontDoorConfig) error {
	domain := wildcardDomain(cfg.BaseDomain)
	existing, err := d.describeTencentCDNDomain(domain)
	if err != nil {
		return err
	}
	if existing != nil {
		rootDomain, subdomain, err := splitWildcardDomain(domain)
		if err != nil {
			return err
		}
		cname := strings.TrimSpace(stringValue(existing.Cname))
		if err := d.deleteTencentDNSRecordByValue(ctx, rootDomain, subdomain, "CNAME", cname); err != nil {
			return err
		}
		if err := d.deleteTencentCDNDomain(ctx, domain); err != nil {
			return err
		}
		return nil
	}
	rootDomain, subdomain, err := splitWildcardDomain(domain)
	if err != nil {
		return err
	}
	return d.deleteTencentDNSRecordByValue(ctx, rootDomain, subdomain, "CNAME", "")
}

func (d *tencentFrontDoor) ensureTencentCDNDomain(ctx context.Context, domain string, originHost string) (string, error) {
	existing, err := d.describeTencentCDNDomain(domain)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if err := validateTencentOrigin(existing.Origin, originHost); err != nil {
			return "", err
		}
		cname := strings.TrimSpace(stringValue(existing.Cname))
		if cname == "" {
			return "", fmt.Errorf("Tencent CDN domain %s exists but has no CNAME", domain)
		}
		return strings.TrimSuffix(cname, "."), nil
	}
	if err := d.ensureTencentDomainVerification(ctx, domain); err != nil {
		return "", err
	}

	request := cdntencent.NewAddCdnDomainRequest()
	request.Domain = &domain
	request.ServiceType = stringPtr("web")
	request.Area = stringPtr("mainland")
	request.Origin = &cdntencent.Origin{
		Origins:            []*string{&originHost},
		OriginType:         stringPtr("domain"),
		OriginPullProtocol: stringPtr("http"),
	}
	request.HttpsBilling = &cdntencent.HttpsBilling{Switch: stringPtr("off")}
	if _, err := d.cdn.AddCdnDomain(request); err != nil {
		return "", fmt.Errorf("add Tencent CDN domain %s: %w", domain, err)
	}

	existing, err = d.describeTencentCDNDomain(domain)
	if err != nil {
		return "", err
	}
	if existing == nil || strings.TrimSpace(stringValue(existing.Cname)) == "" {
		return "", fmt.Errorf("Tencent CDN domain %s created but CNAME is not ready", domain)
	}
	return strings.TrimSuffix(strings.TrimSpace(stringValue(existing.Cname)), "."), nil
}

func (d *tencentFrontDoor) ensureTencentDomainVerification(ctx context.Context, domain string) error {
	request := cdntencent.NewCreateVerifyRecordRequest()
	request.Domain = &domain
	response, err := d.cdn.CreateVerifyRecord(request)
	if err != nil {
		return fmt.Errorf("create Tencent CDN domain verification record for %s: %w", domain, err)
	}
	if response == nil || response.Response == nil {
		return fmt.Errorf("create Tencent CDN domain verification record for %s: empty response", domain)
	}
	recordType := strings.TrimSpace(stringValue(response.Response.RecordType))
	recordName := strings.Trim(strings.TrimSpace(stringValue(response.Response.SubDomain)), ".")
	recordValue := strings.TrimSpace(stringValue(response.Response.Record))
	if recordType == "" || recordName == "" || recordValue == "" {
		return fmt.Errorf("create Tencent CDN domain verification record for %s: incomplete response", domain)
	}
	rootDomain, _, err := splitWildcardDomain(domain)
	if err != nil {
		return err
	}
	subdomain := dnsSubdomainForRoot(recordName, rootDomain)
	recordID, err := d.ensureTencentDNSRecord(ctx, rootDomain, subdomain, recordType, recordValue)
	if err != nil {
		return err
	}
	defer func() { _ = d.deleteTencentDNSRecord(context.Background(), rootDomain, recordID) }()

	verify := cdntencent.NewVerifyDomainRecordRequest()
	verify.Domain = &domain
	verify.VerifyType = stringPtr("dns")
	verifyResponse, err := d.cdn.VerifyDomainRecord(verify)
	if err != nil {
		return fmt.Errorf("verify Tencent CDN domain %s: %w", domain, err)
	}
	if verifyResponse == nil || verifyResponse.Response == nil || !boolValue(verifyResponse.Response.Result) {
		return fmt.Errorf("Tencent CDN domain %s DNS verification is pending", domain)
	}
	return nil
}

func (d *tencentFrontDoor) describeTencentCDNDomain(domain string) (*cdntencent.DetailDomain, error) {
	request := cdntencent.NewDescribeDomainsConfigRequest()
	request.Filters = []*cdntencent.DomainFilter{{
		Name:  stringPtr("domain"),
		Value: []*string{&domain},
		Fuzzy: boolPtr(false),
	}}
	response, err := d.cdn.DescribeDomainsConfig(request)
	if err != nil {
		return nil, fmt.Errorf("describe Tencent CDN domain %s: %w", domain, err)
	}
	if response.Response == nil {
		return nil, nil
	}
	for _, item := range response.Response.Domains {
		if item != nil && strings.EqualFold(strings.TrimSpace(stringValue(item.Domain)), domain) {
			return item, nil
		}
	}
	return nil, nil
}

func validateTencentOrigin(origin *cdntencent.Origin, originHost string) error {
	if origin == nil {
		return fmt.Errorf("Tencent CDN origin is empty")
	}
	if strings.TrimSpace(stringValue(origin.OriginType)) != "domain" {
		return fmt.Errorf("Tencent CDN origin type is %s, want domain", stringValue(origin.OriginType))
	}
	if protocol := strings.TrimSpace(stringValue(origin.OriginPullProtocol)); protocol != "" && protocol != "http" {
		return fmt.Errorf("Tencent CDN origin pull protocol is %s, want http", protocol)
	}
	for _, item := range origin.Origins {
		if sameDNSValue(stringValue(item), originHost) {
			return nil
		}
	}
	return fmt.Errorf("Tencent CDN origin is not %s", originHost)
}

func (d *tencentFrontDoor) ensureTencentDNSRecord(ctx context.Context, domain string, subdomain string, recordType string, recordValue string) (uint64, error) {
	record, err := d.tencentDNSRecord(ctx, domain, subdomain, recordType)
	if err != nil {
		return 0, err
	}
	if record != nil {
		if sameDNSValue(stringValue(record.Value), recordValue) {
			return uint64Value(record.RecordId), nil
		}
		return 0, fmt.Errorf("%s.%s already exists as %s %s", subdomain, domain, stringValue(record.Type), stringValue(record.Value))
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	create := dnspod.NewCreateRecordRequest()
	create.Domain = &domain
	create.SubDomain = &subdomain
	create.RecordType = &recordType
	create.RecordLine = stringPtr("默认")
	create.Value = &recordValue
	create.TTL = uint64Ptr(600)
	create.Remark = stringPtr("mini-cloud lab front door")
	createResponse, err := d.dns.CreateRecord(create)
	if err != nil {
		return 0, fmt.Errorf("create DNSPod %s %s.%s: %w", recordType, subdomain, domain, err)
	}
	if createResponse == nil || createResponse.Response == nil || createResponse.Response.RecordId == nil {
		return 0, fmt.Errorf("create DNSPod %s %s.%s: empty record id", recordType, subdomain, domain)
	}
	return *createResponse.Response.RecordId, nil
}

func (d *tencentFrontDoor) tencentDNSRecord(ctx context.Context, domain string, subdomain string, recordType string) (*dnspod.RecordListItem, error) {
	request := dnspod.NewDescribeRecordListRequest()
	request.Domain = &domain
	request.Subdomain = &subdomain
	request.RecordType = &recordType
	request.ErrorOnEmpty = stringPtr("no")
	response, err := d.dns.DescribeRecordListWithContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("describe DNSPod record %s.%s: %w", subdomain, domain, err)
	}
	if response.Response == nil {
		return nil, nil
	}
	for _, record := range response.Response.RecordList {
		if record != nil {
			return record, nil
		}
	}
	return nil, nil
}

func (d *tencentFrontDoor) deleteTencentDNSRecordByValue(ctx context.Context, domain string, subdomain string, recordType string, value string) error {
	record, err := d.tencentDNSRecord(ctx, domain, subdomain, recordType)
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if value != "" && !sameDNSValue(stringValue(record.Value), value) {
		return fmt.Errorf("refuse to delete DNSPod %s.%s because value is %s, not %s", subdomain, domain, stringValue(record.Value), value)
	}
	return d.deleteTencentDNSRecord(ctx, domain, uint64Value(record.RecordId))
}

func (d *tencentFrontDoor) deleteTencentDNSRecord(ctx context.Context, domain string, recordID uint64) error {
	if recordID == 0 {
		return nil
	}
	request := dnspod.NewDeleteRecordRequest()
	request.Domain = &domain
	request.RecordId = &recordID
	if _, err := d.dns.DeleteRecordWithContext(ctx, request); err != nil {
		return fmt.Errorf("delete DNSPod record %d for %s: %w", recordID, domain, err)
	}
	return nil
}

func (d *tencentFrontDoor) deleteTencentCDNDomain(ctx context.Context, domain string) error {
	for i := 0; i < 24; i++ {
		existing, err := d.describeTencentCDNDomain(domain)
		if err != nil {
			return err
		}
		if existing == nil {
			return nil
		}
		if strings.EqualFold(strings.TrimSpace(stringValue(existing.Status)), "offline") {
			break
		}
		if i == 0 {
			stop := cdntencent.NewStopCdnDomainRequest()
			stop.Domain = &domain
			if _, err := d.cdn.StopCdnDomainWithContext(ctx, stop); err != nil && !isTencentResourceNotFound(err) {
				return fmt.Errorf("stop Tencent CDN domain %s: %w", domain, err)
			}
		}
		time.Sleep(5 * time.Second)
	}
	request := cdntencent.NewDeleteCdnDomainRequest()
	request.Domain = &domain
	if _, err := d.cdn.DeleteCdnDomainWithContext(ctx, request); err != nil && !isTencentResourceNotFound(err) {
		return fmt.Errorf("delete Tencent CDN domain %s: %w", domain, err)
	}
	return nil
}

func isTencentResourceNotFound(err error) bool {
	var sdkErr *tcerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := strings.ToLower(sdkErr.Code)
		return strings.Contains(code, "notfound") || strings.Contains(code, "notexist")
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "notfound") || strings.Contains(lower, "not exist") || strings.Contains(lower, "not found")
}

type aliyunFrontDoor struct {
	cdn *cdnali.Client
	dns *alidns.Client
}

func newAliyunFrontDoor() (*aliyunFrontDoor, error) {
	credential, err := credentials.NewCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create Aliyun credential: %w", err)
	}
	cdnClient, err := cdnali.NewClient(&openapi.Config{
		Endpoint:   tea.String("cdn.aliyuncs.com"),
		Credential: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("create Aliyun CDN client: %w", err)
	}
	dnsClient, err := alidns.NewClient(&openapi.Config{
		Endpoint:   tea.String("alidns.aliyuncs.com"),
		Credential: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("create AliDNS client: %w", err)
	}
	return &aliyunFrontDoor{cdn: cdnClient, dns: dnsClient}, nil
}

func (d *aliyunFrontDoor) ensure(ctx context.Context, cfg frontDoorConfig) error {
	domain := wildcardDomain(cfg.BaseDomain)
	cname, err := d.ensureAliyunCDNDomain(aliyunCDNWildcard(domain), cfg.OriginHost)
	if err != nil {
		return err
	}
	rootDomain, rr, err := splitWildcardDomain(domain)
	if err != nil {
		return err
	}
	return d.ensureAliyunDNSCNAME(ctx, rootDomain, rr, cname)
}

func (d *aliyunFrontDoor) delete(ctx context.Context, cfg frontDoorConfig) error {
	domain := aliyunCDNWildcard(wildcardDomain(cfg.BaseDomain))
	existing, err := d.describeAliyunCDNDomain(domain)
	if err != nil {
		return err
	}
	rootDomain, rr, err := splitWildcardDomain(wildcardDomain(cfg.BaseDomain))
	if err != nil {
		return err
	}
	if existing != nil {
		if err := d.deleteAliyunDNSCNAME(ctx, rootDomain, rr, stringValue(existing.Cname)); err != nil {
			return err
		}
		if err := d.deleteAliyunCDNDomain(domain); err != nil {
			return err
		}
		return nil
	}
	return d.deleteAliyunDNSCNAME(ctx, rootDomain, rr, "")
}

func (d *aliyunFrontDoor) ensureAliyunCDNDomain(domain string, originHost string) (string, error) {
	existing, err := d.describeAliyunCDNDomain(domain)
	if err != nil {
		return "", err
	}
	if existing != nil {
		cname := strings.TrimSpace(stringValue(existing.Cname))
		if cname == "" {
			return "", fmt.Errorf("Aliyun CDN domain %s exists but has no CNAME", domain)
		}
		return strings.TrimSuffix(cname, "."), nil
	}

	sources, err := json.Marshal([]map[string]any{{
		"content":  originHost,
		"type":     "domain",
		"priority": "20",
		"port":     80,
		"weight":   "10",
	}})
	if err != nil {
		return "", err
	}
	if _, err := d.cdn.AddCdnDomain(&cdnali.AddCdnDomainRequest{
		DomainName: &domain,
		CdnType:    tea.String("web"),
		Scope:      tea.String("domestic"),
		Sources:    tea.String(string(sources)),
	}); err != nil {
		return "", fmt.Errorf("add Aliyun CDN domain %s: %w", domain, err)
	}

	existing, err = d.describeAliyunCDNDomain(domain)
	if err != nil {
		return "", err
	}
	if existing == nil || strings.TrimSpace(stringValue(existing.Cname)) == "" {
		return "", fmt.Errorf("Aliyun CDN domain %s created but CNAME is not ready", domain)
	}
	return strings.TrimSuffix(strings.TrimSpace(stringValue(existing.Cname)), "."), nil
}

func (d *aliyunFrontDoor) describeAliyunCDNDomain(domain string) (*cdnali.DescribeUserDomainsResponseBodyDomainsPageData, error) {
	response, err := d.cdn.DescribeUserDomains(&cdnali.DescribeUserDomainsRequest{
		DomainName:       &domain,
		DomainSearchType: tea.String("full_match"),
		PageNumber:       tea.Int32(1),
		PageSize:         tea.Int32(20),
	})
	if err != nil {
		return nil, fmt.Errorf("describe Aliyun CDN domain %s: %w", domain, err)
	}
	if response == nil || response.Body == nil || response.Body.Domains == nil {
		return nil, nil
	}
	for _, item := range response.Body.Domains.PageData {
		if item != nil && strings.EqualFold(strings.TrimSpace(stringValue(item.DomainName)), domain) {
			return item, nil
		}
	}
	return nil, nil
}

func (d *aliyunFrontDoor) ensureAliyunDNSCNAME(ctx context.Context, domain string, rr string, cname string) error {
	record, err := d.aliyunDNSRecord(domain, rr, "CNAME")
	if err != nil {
		return err
	}
	if record != nil {
		if sameDNSValue(stringValue(record.Value), cname) {
			return nil
		}
		return fmt.Errorf("%s.%s already exists as %s %s", rr, domain, stringValue(record.Type), stringValue(record.Value))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := d.dns.AddDomainRecord(&alidns.AddDomainRecordRequest{
		DomainName: &domain,
		RR:         &rr,
		Type:       tea.String("CNAME"),
		Value:      &cname,
		TTL:        tea.Int64(600),
		Line:       tea.String("default"),
	}); err != nil {
		return fmt.Errorf("create AliDNS CNAME %s.%s: %w", rr, domain, err)
	}
	return nil
}

func (d *aliyunFrontDoor) deleteAliyunDNSCNAME(ctx context.Context, domain string, rr string, value string) error {
	record, err := d.aliyunDNSRecord(domain, rr, "CNAME")
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if value != "" && !sameDNSValue(stringValue(record.Value), value) {
		return fmt.Errorf("refuse to delete AliDNS %s.%s because value is %s, not %s", rr, domain, stringValue(record.Value), value)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := d.dns.DeleteDomainRecord(&alidns.DeleteDomainRecordRequest{RecordId: record.RecordId}); err != nil {
		return fmt.Errorf("delete AliDNS record %s for %s.%s: %w", stringValue(record.RecordId), rr, domain, err)
	}
	return nil
}

func (d *aliyunFrontDoor) aliyunDNSRecord(domain string, rr string, recordType string) (*alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord, error) {
	response, err := d.dns.DescribeDomainRecords(&alidns.DescribeDomainRecordsRequest{
		DomainName:  &domain,
		RRKeyWord:   &rr,
		TypeKeyWord: &recordType,
		PageNumber:  tea.Int64(1),
		PageSize:    tea.Int64(20),
	})
	if err != nil {
		return nil, fmt.Errorf("describe AliDNS record %s.%s: %w", rr, domain, err)
	}
	if response == nil || response.Body == nil || response.Body.DomainRecords == nil {
		return nil, nil
	}
	for _, record := range response.Body.DomainRecords.Record {
		if record != nil && strings.EqualFold(stringValue(record.RR), rr) {
			return record, nil
		}
	}
	return nil, nil
}

func (d *aliyunFrontDoor) deleteAliyunCDNDomain(domain string) error {
	existing, err := d.describeAliyunCDNDomain(domain)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(stringValue(existing.DomainStatus)), "online") {
		if _, err := d.cdn.StopCdnDomain(&cdnali.StopCdnDomainRequest{DomainName: &domain}); err != nil && !isAliyunNotFound(err) {
			return fmt.Errorf("stop Aliyun CDN domain %s: %w", domain, err)
		}
	}
	if _, err := d.cdn.DeleteCdnDomain(&cdnali.DeleteCdnDomainRequest{DomainName: &domain}); err != nil && !isAliyunNotFound(err) {
		return fmt.Errorf("delete Aliyun CDN domain %s: %w", domain, err)
	}
	return nil
}

func isAliyunNotFound(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "notfound") || strings.Contains(lower, "not exist") || strings.Contains(lower, "not found")
}

func wildcardDomain(baseDomain string) string {
	baseDomain = strings.Trim(strings.TrimSpace(baseDomain), ".")
	if baseDomain == "" {
		return ""
	}
	return "*." + baseDomain
}

func splitWildcardDomain(domain string) (string, string, error) {
	domain = strings.Trim(strings.TrimSpace(domain), ".")
	if !strings.HasPrefix(domain, "*.") {
		return "", "", fmt.Errorf("wildcard domain %s must start with *.", domain)
	}
	rest := strings.TrimPrefix(domain, "*.")
	parts := strings.Split(rest, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("wildcard domain %s is invalid", domain)
	}
	root := strings.Join(parts[len(parts)-2:], ".")
	subdomain := "*." + strings.Join(parts[:len(parts)-2], ".")
	if subdomain == "*." {
		subdomain = "*"
	}
	return root, subdomain, nil
}

func dnsSubdomainForRoot(recordName string, rootDomain string) string {
	recordName = strings.Trim(strings.TrimSpace(recordName), ".")
	rootDomain = strings.Trim(strings.TrimSpace(rootDomain), ".")
	if strings.EqualFold(recordName, rootDomain) {
		return "@"
	}
	suffix := "." + rootDomain
	if strings.HasSuffix(strings.ToLower(recordName), strings.ToLower(suffix)) {
		return strings.TrimSuffix(recordName[:len(recordName)-len(suffix)], ".")
	}
	return recordName
}

func aliyunCDNWildcard(domain string) string {
	return "." + strings.TrimPrefix(strings.Trim(strings.TrimSpace(domain), "."), "*.")
}

func sameDNSValue(left string, right string) bool {
	return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(left), "."), strings.TrimSuffix(strings.TrimSpace(right), "."))
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func uint64Ptr(value uint64) *uint64 { return &value }

func stringValue(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}

func uint64Value(input *uint64) uint64 {
	if input == nil {
		return 0
	}
	return *input
}

func boolValue(input *bool) bool {
	if input == nil {
		return false
	}
	return *input
}
