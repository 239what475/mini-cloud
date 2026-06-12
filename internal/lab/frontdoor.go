package lab

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type aliyunCDNDomainsResponse struct {
	Domains struct {
		PageData []struct {
			DomainName   string `json:"DomainName"`
			DomainStatus string `json:"DomainStatus"`
		} `json:"PageData"`
	} `json:"Domains"`
}

type tencentCDNDomainsResponse struct {
	Domains []struct {
		Domain string `json:"Domain"`
	} `json:"Domains"`
}

type dnspodRecordListResponse struct {
	RecordList []struct {
		RecordID uint64 `json:"RecordId"`
		Name     string `json:"Name"`
		Type     string `json:"Type"`
		Value    string `json:"Value"`
	} `json:"RecordList"`
}

func (r *Runner) deleteServiceFrontDoors(ctx context.Context, out TerraformOutput) error {
	return r.deleteServiceFrontDoorsForProvider(ctx, out.ProviderName(), out.RegionID())
}

func (r *Runner) deleteServiceFrontDoorsWithoutTerraform(ctx context.Context, plane Plane) error {
	return r.deleteServiceFrontDoorsForProvider(ctx, plane.Provider, plane.Region)
}

func (r *Runner) deleteServiceFrontDoorsForProvider(ctx context.Context, provider string, regionID string) error {
	baseDomain := cleanLabDomain(r.cfg.Install.IngressBaseDomain)
	if baseDomain == "" {
		return nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "aliyun":
		if err := r.deleteAliyunCDNDomains(ctx, baseDomain); err != nil {
			return err
		}
		if err := r.deleteAliyunVerifyTXTRecord(ctx, baseDomain); err != nil {
			return err
		}
	case "tencent":
		regionID = strings.TrimSpace(regionID)
		if regionID != "" {
			if err := r.deleteTencentCDNDomains(ctx, regionID, baseDomain); err != nil {
				return err
			}
		}
		if err := r.deleteTencentVerifyTXTRecord(ctx, baseDomain); err != nil {
			return err
		}
	}
	return r.deleteDNSPodCNAMERecords(ctx, baseDomain, provider)
}

func (r *Runner) deleteAliyunCDNDomains(ctx context.Context, baseDomain string) error {
	var response aliyunCDNDomainsResponse
	if err := runJSON(ctx, &response, "aliyun", "cdn", "DescribeUserDomains",
		"--DomainName", baseDomain,
		"--DomainSearchType", "suf_match",
		"--PageNumber", "1",
		"--PageSize", "500",
	); err != nil {
		return err
	}
	for _, item := range response.Domains.PageData {
		host := cleanLabDomain(item.DomainName)
		if !labDomainIsUnder(host, baseDomain) {
			continue
		}
		if err := r.deleteAliyunCDNDomain(ctx, host); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteAliyunCDNDomain(ctx context.Context, host string) error {
	const retryInterval = 15 * time.Second
	deadline := time.Now().Add(10 * time.Minute)

	for {
		status, found, err := r.aliyunCDNDomainStatus(ctx, host)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}

		switch strings.ToLower(status) {
		case "online":
			_, err = runOutput(ctx, "aliyun", "cdn", "StopCdnDomain", "--DomainName", host)
			if err != nil && !aliyunCDNDeleteRetryable(err) && !commandOutputIndicatesMissingResource(err) {
				return err
			}
		case "offline":
			_, err = runOutput(ctx, "aliyun", "cdn", "DeleteCdnDomain", "--DomainName", host)
			if err == nil || commandOutputIndicatesMissingResource(err) {
				return nil
			}
			if !aliyunCDNDeleteRetryable(err) {
				return err
			}
		default:
			err = fmt.Errorf("aliyun CDN domain %s is %s", host, status)
		}

		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("delete aliyun CDN domain %s timed out", host)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}

func (r *Runner) aliyunCDNDomainStatus(ctx context.Context, host string) (string, bool, error) {
	var response aliyunCDNDomainsResponse
	if err := runJSON(ctx, &response, "aliyun", "cdn", "DescribeUserDomains",
		"--DomainName", host,
		"--DomainSearchType", "full_match",
		"--PageNumber", "1",
		"--PageSize", "10",
	); err != nil {
		return "", false, err
	}
	for _, item := range response.Domains.PageData {
		if cleanLabDomain(item.DomainName) == host {
			return strings.TrimSpace(item.DomainStatus), true, nil
		}
	}
	return "", false, nil
}

func (r *Runner) deleteTencentCDNDomains(ctx context.Context, regionID string, baseDomain string) error {
	var response tencentCDNDomainsResponse
	filter := fmt.Sprintf(`[{"Name":"domain","Value":["%s"],"Fuzzy":true}]`, baseDomain)
	if err := runJSON(ctx, &response, "tccli", "cdn", "DescribeDomains", "--region", regionID, "--Filters", filter); err != nil {
		return err
	}
	for _, item := range response.Domains {
		host := cleanLabDomain(item.Domain)
		if !labDomainIsUnder(host, baseDomain) {
			continue
		}
		if _, err := runOutput(ctx, "tccli", "cdn", "StopCdnDomain", "--region", regionID, "--Domain", host); err != nil && !commandOutputIndicatesMissingResource(err) {
			return err
		}
		if _, err := runOutput(ctx, "tccli", "cdn", "DeleteCdnDomain", "--region", regionID, "--Domain", host); err != nil && !commandOutputIndicatesMissingResource(err) {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteDNSPodCNAMERecords(ctx context.Context, baseDomain string, provider string) error {
	dnsRoot := cleanLabDomain(rootDomain(baseDomain))
	if dnsRoot == "" {
		return nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	var response dnspodRecordListResponse
	if err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList",
		"--cli-unfold-argument",
		"--Domain", dnsRoot,
		"--RecordType", "CNAME",
		"--ErrorOnEmpty", "no",
	); err != nil {
		return err
	}
	for _, item := range response.RecordList {
		if strings.ToUpper(strings.TrimSpace(item.Type)) != "CNAME" {
			continue
		}
		host := dnsPodRecordHost(item.Name, dnsRoot)
		if !labDomainIsUnder(host, baseDomain) {
			continue
		}
		if !providerOwnsCNAME(provider, item.Value) {
			continue
		}
		if _, err := runOutput(ctx, "tccli", "dnspod", "DeleteRecord",
			"--cli-unfold-argument",
			"--Domain", dnsRoot,
			"--RecordId", fmt.Sprintf("%d", item.RecordID),
		); err != nil && !commandOutputIndicatesMissingResource(err) {
			return err
		}
	}
	return nil
}

func providerOwnsCNAME(provider string, value string) bool {
	value = cleanLabDomain(value)
	switch provider {
	case "aliyun":
		return strings.HasSuffix(value, ".w.kunlunaq.com")
	case "tencent":
		return strings.HasSuffix(value, ".cdn.dnsv1.com")
	default:
		return false
	}
}

func (r *Runner) deleteAliyunVerifyTXTRecord(ctx context.Context, baseDomain string) error {
	dnsRoot := cleanLabDomain(rootDomain(baseDomain))
	if dnsRoot == "" {
		return nil
	}
	var response dnspodRecordListResponse
	if err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList",
		"--cli-unfold-argument",
		"--Domain", dnsRoot,
		"--Subdomain", "verification",
		"--RecordType", "TXT",
		"--ErrorOnEmpty", "no",
	); err != nil {
		return err
	}
	for _, item := range response.RecordList {
		if strings.TrimSpace(item.Name) != "verification" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(item.Type)) != "TXT" || !strings.HasPrefix(strings.TrimSpace(item.Value), "verify_") {
			continue
		}
		if _, err := runOutput(ctx, "tccli", "dnspod", "DeleteRecord",
			"--cli-unfold-argument",
			"--Domain", dnsRoot,
			"--RecordId", fmt.Sprintf("%d", item.RecordID),
		); err != nil && !commandOutputIndicatesMissingResource(err) {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteTencentVerifyTXTRecord(ctx context.Context, baseDomain string) error {
	dnsRoot := cleanLabDomain(rootDomain(baseDomain))
	if dnsRoot == "" {
		return nil
	}
	var response dnspodRecordListResponse
	if err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList",
		"--cli-unfold-argument",
		"--Domain", dnsRoot,
		"--Subdomain", "_cdnauth",
		"--RecordType", "TXT",
		"--ErrorOnEmpty", "no",
	); err != nil {
		return err
	}
	for _, item := range response.RecordList {
		if strings.TrimSpace(item.Name) != "_cdnauth" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(item.Type)) != "TXT" {
			continue
		}
		if _, err := runOutput(ctx, "tccli", "dnspod", "DeleteRecord",
			"--cli-unfold-argument",
			"--Domain", dnsRoot,
			"--RecordId", fmt.Sprintf("%d", item.RecordID),
		); err != nil && !commandOutputIndicatesMissingResource(err) {
			return err
		}
	}
	return nil
}

func dnsPodRecordHost(name string, root string) string {
	name = strings.Trim(strings.TrimSpace(name), ".")
	root = cleanLabDomain(root)
	if name == "" || name == "@" {
		return root
	}
	return cleanLabDomain(name + "." + root)
}

func cleanLabDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func labDomainIsUnder(child string, parent string) bool {
	child = cleanLabDomain(child)
	parent = cleanLabDomain(parent)
	return child == parent || strings.HasSuffix(child, "."+parent)
}

func commandOutputContains(err error, fragment string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(fragment))
}

func commandOutputIndicatesMissingResource(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "notfound") ||
		strings.Contains(message, "not exist") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "not exists") ||
		strings.Contains(message, "resourcenotfound") ||
		strings.Contains(message, "domainnotexist")
}

func aliyunCDNDeleteRetryable(err error) bool {
	return commandOutputContains(err, "servicebusy") ||
		commandOutputContains(err, "configuring") ||
		commandOutputContains(err, "processing")
}
