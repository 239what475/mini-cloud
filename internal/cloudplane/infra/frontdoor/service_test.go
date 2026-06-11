package frontdoor

import (
	"context"
	"strings"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestServiceApplyEnsuresDesiredDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if cdn.domains["api.apps.example.com"] != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("CDN domains = %+v", cdn.domains)
	}
	record := dns.records["api.apps.example.com"]
	if record.Value != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("DNS record = %+v", record)
	}
}

func TestServiceApplyWaitsForCDNCNAME(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{
		domains: map[string]string{},
		pending: map[string]bool{"api.apps.example.com": true},
	}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(dns.records) != 0 {
		t.Fatalf("DNS record was written before CDN CNAME was ready: %+v", dns.records)
	}
}

func TestServiceApplyDeletesStaleDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{
		"api.apps.example.com": "api.apps.example.com.cdn.example.net",
		"old.apps.example.com": "old.apps.example.com.cdn.example.net",
		"other.example.com":    "other.example.com.cdn.example.net",
	}}
	dns := &fakeDNS{records: map[string]DNSRecord{
		"api.apps.example.com":   {ID: 1, Subdomain: "api.apps.example.com", Type: "CNAME", Value: "api.apps.example.com.cdn.example.net"},
		"old.apps.example.com":   {ID: 2, Subdomain: "old.apps.example.com", Type: "CNAME", Value: "old.apps.example.com.cdn.example.net"},
		"peer.apps.example.com":  {ID: 6, Subdomain: "peer.apps.example.com", Type: "CNAME", Value: "peer.apps.example.com.other-cdn.example.net"},
		"other.example.com":      {ID: 3, Subdomain: "other.example.com", Type: "CNAME", Value: "other.example.com.cdn.example.net"},
		"txt.apps.example.com":   {ID: 4, Subdomain: "txt.apps.example.com", Type: "TXT", Value: "keep"},
		"plain.apps.example.com": {ID: 5, Subdomain: "plain.apps.example.com", Type: "A", Value: "192.0.2.1"},
	}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if _, ok := cdn.domains["old.apps.example.com"]; ok {
		t.Fatalf("stale CDN domain was not deleted: %+v", cdn.domains)
	}
	if _, ok := cdn.domains["other.example.com"]; !ok {
		t.Fatalf("CDN domain outside base domain was deleted: %+v", cdn.domains)
	}
	if _, ok := dns.records["old.apps.example.com"]; ok {
		t.Fatalf("stale DNS record was not deleted: %+v", dns.records)
	}
	if _, ok := dns.records["other.example.com"]; !ok {
		t.Fatalf("DNS record outside base domain was deleted: %+v", dns.records)
	}
	if _, ok := dns.records["peer.apps.example.com"]; !ok {
		t.Fatalf("DNS record owned by another CDN was deleted: %+v", dns.records)
	}
	if _, ok := dns.records["txt.apps.example.com"]; !ok {
		t.Fatalf("non-CNAME DNS record was deleted: %+v", dns.records)
	}
}

type fakeCDN struct {
	domains map[string]string
	pending map[string]bool
}

func (f *fakeCDN) ListDomains(context.Context, string) ([]CDNDomain, error) {
	domains := make([]CDNDomain, 0, len(f.domains))
	for host, cname := range f.domains {
		domains = append(domains, CDNDomain{Host: host, CNAME: cname})
	}
	return domains, nil
}

func (f *fakeCDN) PrepareDomain(context.Context, string, dnsClient) error {
	return nil
}

func (f *fakeCDN) EnsureDomain(_ context.Context, host string) (string, error) {
	if f.pending[host] {
		return "", nil
	}
	if f.domains == nil {
		f.domains = map[string]string{}
	}
	if f.domains[host] == "" {
		f.domains[host] = host + ".cdn.example.net"
	}
	return f.domains[host], nil
}

func (f *fakeCDN) DeleteDomain(_ context.Context, host string) error {
	delete(f.domains, host)
	return nil
}

func (f *fakeCDN) OwnsCNAME(value string) bool {
	return trimCNAME(value) == "" || strings.HasSuffix(trimCNAME(value), ".cdn.example.net")
}

type fakeDNS struct {
	nextID  uint64
	records map[string]DNSRecord
}

func (f *fakeDNS) ListRecords(context.Context) ([]DNSRecord, error) {
	records := make([]DNSRecord, 0, len(f.records))
	for _, record := range f.records {
		records = append(records, record)
	}
	return records, nil
}

func (f *fakeDNS) EnsureRecord(_ context.Context, host string, recordType string, value string) error {
	if f.records == nil {
		f.records = map[string]DNSRecord{}
	}
	f.nextID++
	f.records[host] = DNSRecord{ID: f.nextID, Subdomain: host, Type: recordType, Value: value}
	return nil
}

func (f *fakeDNS) DeleteRecord(_ context.Context, record DNSRecord) error {
	delete(f.records, record.Subdomain)
	return nil
}
