package lab

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type dnsRecordResponse struct {
	RecordList []struct {
		RecordID int64  `json:"RecordId"`
		Value    string `json:"Value"`
	} `json:"RecordList"`
}

func (r *Runner) ensureDNSRecord(ctx context.Context) error {
	domain := strings.TrimSpace(r.cfg.DNS.Domain)
	subdomain := strings.TrimSpace(r.cfg.DNS.Subdomain)
	value := strings.TrimSpace(r.cfg.DNS.Value)
	if domain == "" || subdomain == "" || value == "" {
		return nil
	}
	record, err := r.dnsRecord(ctx, domain, subdomain)
	if err != nil {
		return err
	}
	if record.RecordID != 0 {
		if record.Value == value || record.Value == value+"." {
			return nil
		}
		return fmt.Errorf("%s.%s already exists with value %s", subdomain, domain, record.Value)
	}
	_, err = runOutput(ctx, "tccli", "dnspod", "CreateRecord",
		"--cli-unfold-argument",
		"--Domain", domain,
		"--SubDomain", subdomain,
		"--RecordType", "CNAME",
		"--RecordLine", "默认",
		"--Value", value,
		"--TTL", "600",
		"--Remark", "mini-cloud-lab",
	)
	return err
}

func (r *Runner) deleteDNSRecord(ctx context.Context) error {
	domain := strings.TrimSpace(r.cfg.DNS.Domain)
	subdomain := strings.TrimSpace(r.cfg.DNS.Subdomain)
	expectedValue := strings.TrimSpace(r.cfg.DNS.Value)
	if domain == "" || subdomain == "" {
		return nil
	}
	record, err := r.dnsRecord(ctx, domain, subdomain)
	if err != nil {
		return err
	}
	if record.RecordID == 0 {
		return nil
	}
	if expectedValue != "" && record.Value != expectedValue && record.Value != expectedValue+"." {
		return fmt.Errorf("%s.%s points to %s; refusing to delete it", subdomain, domain, record.Value)
	}
	_, err = runOutput(ctx, "tccli", "dnspod", "DeleteRecord",
		"--cli-unfold-argument",
		"--Domain", domain,
		"--RecordId", strconv.FormatInt(record.RecordID, 10),
	)
	return err
}

func (r *Runner) dnsRecord(ctx context.Context, domain string, subdomain string) (struct {
	RecordID int64
	Value    string
}, error) {
	var empty struct {
		RecordID int64
		Value    string
	}
	var response dnsRecordResponse
	err := runJSON(ctx, &response, "tccli", "dnspod", "DescribeRecordList", "--cli-unfold-argument", "--Domain", domain, "--Subdomain", subdomain)
	if err != nil {
		return empty, err
	}
	if len(response.RecordList) == 0 {
		return empty, nil
	}
	return struct {
		RecordID int64
		Value    string
	}{RecordID: response.RecordList[0].RecordID, Value: response.RecordList[0].Value}, nil
}
