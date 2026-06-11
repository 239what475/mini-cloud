package lab

import (
	"context"
	"fmt"
	"os"
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
	baseDomain := cleanLabDomain(r.cfg.Install.IngressBaseDomain)
	if baseDomain == "" {
		return nil
	}
	switch out.ProviderName() {
	case "aliyun":
		if err := r.deleteAliyunCDNDomains(ctx, baseDomain); err != nil {
			return err
		}
		if err := r.deleteAliyunVerifyTXTRecord(ctx, baseDomain); err != nil {
			return err
		}
	case "tencent":
		if err := r.deleteTencentCDNDomains(ctx, out.RegionID(), baseDomain); err != nil {
			return err
		}
	}
	return r.deleteDNSPodCNAMERecords(ctx, baseDomain)
}

func (r *Runner) deleteServiceFrontDoorsWithoutTerraform(ctx context.Context) error {
	baseDomain := cleanLabDomain(r.cfg.Install.IngressBaseDomain)
	if baseDomain == "" {
		return nil
	}
	provider, err := r.labProviderName()
	if err != nil {
		return err
	}
	switch provider {
	case "aliyun":
		if err := r.deleteAliyunCDNDomains(ctx, baseDomain); err != nil {
			return err
		}
		if err := r.deleteAliyunVerifyTXTRecord(ctx, baseDomain); err != nil {
			return err
		}
	case "tencent":
		regionID, err := r.labRegionID()
		if err != nil {
			return err
		}
		if regionID != "" {
			if err := r.deleteTencentCDNDomains(ctx, regionID, baseDomain); err != nil {
				return err
			}
		}
	}
	return r.deleteDNSPodCNAMERecords(ctx, baseDomain)
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
			if err != nil && !aliyunCDNDeleteRetryable(err) && !commandOutputContains(err, "not") {
				return err
			}
		case "offline":
			_, err = runOutput(ctx, "aliyun", "cdn", "DeleteCdnDomain", "--DomainName", host)
			if err == nil || commandOutputContains(err, "not") {
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
		if _, err := runOutput(ctx, "tccli", "cdn", "StopCdnDomain", "--region", regionID, "--Domain", host); err != nil && !commandOutputContains(err, "not") {
			return err
		}
		if _, err := runOutput(ctx, "tccli", "cdn", "DeleteCdnDomain", "--region", regionID, "--Domain", host); err != nil && !commandOutputContains(err, "not") {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteDNSPodCNAMERecords(ctx context.Context, baseDomain string) error {
	rootDomain := cleanLabDomain(rootDomain(baseDomain))
	if rootDomain == "" {
		return nil
	}
	var response dnspodRecordListResponse
	if err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList",
		"--cli-unfold-argument",
		"--Domain", rootDomain,
		"--RecordType", "CNAME",
		"--ErrorOnEmpty", "no",
	); err != nil {
		return err
	}
	for _, item := range response.RecordList {
		if strings.ToUpper(strings.TrimSpace(item.Type)) != "CNAME" {
			continue
		}
		host := dnsPodRecordHost(item.Name, rootDomain)
		if !labDomainIsUnder(host, baseDomain) {
			continue
		}
		if _, err := runOutput(ctx, "tccli", "dnspod", "DeleteRecord",
			"--cli-unfold-argument",
			"--Domain", rootDomain,
			"--RecordId", fmt.Sprintf("%d", item.RecordID),
		); err != nil && !commandOutputContains(err, "not") {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteAliyunVerifyTXTRecord(ctx context.Context, baseDomain string) error {
	rootDomain := cleanLabDomain(rootDomain(baseDomain))
	if rootDomain == "" {
		return nil
	}
	var response dnspodRecordListResponse
	if err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList",
		"--cli-unfold-argument",
		"--Domain", rootDomain,
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
			"--Domain", rootDomain,
			"--RecordId", fmt.Sprintf("%d", item.RecordID),
		); err != nil && !commandOutputContains(err, "not") {
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

func aliyunCDNDeleteRetryable(err error) bool {
	return commandOutputContains(err, "servicebusy") ||
		commandOutputContains(err, "configuring") ||
		commandOutputContains(err, "processing")
}

func (r *Runner) labProviderName() (string, error) {
	value, err := terraformVariable("provider_name")
	if err != nil {
		return "", err
	}
	return strings.ToLower(value), nil
}

func (r *Runner) labRegionID() (string, error) {
	value, err := terraformMapString("tencent", "region_id")
	if err != nil || value != "" {
		return value, err
	}
	return terraformMapString("aliyun", "region_id")
}

func terraformVariable(name string) (string, error) {
	data, err := os.ReadFile("deploy/terraform/lab/terraform.tfvars")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	prefix := name + " = "
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), `"`), nil
		}
	}
	return "", nil
}

func terraformMapString(mapName string, key string) (string, error) {
	data, err := os.ReadFile("deploy/terraform/lab/terraform.tfvars")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	inMap := false
	prefix := key + " = "
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == mapName+" = {" {
			inMap = true
			continue
		}
		if inMap && line == "}" {
			return "", nil
		}
		if inMap && strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), `"`), nil
		}
	}
	return "", nil
}
