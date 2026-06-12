package frontdoor

import (
	"context"
	"errors"
	"strings"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestServiceApplyEnsuresDesiredDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	store := &fakeDomainStore{domains: map[string]ManagedDomain{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, store)

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
	if store.domains["api.apps.example.com"].CNAME != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("managed frontdoor domains = %+v", store.domains)
	}
}

func TestServiceApplyWaitsForCDNCNAME(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{
		domains: map[string]string{},
		pending: map[string]bool{"api.apps.example.com": true},
	}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, &fakeDomainStore{domains: map[string]ManagedDomain{}})

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(dns.records) != 0 {
		t.Fatalf("DNS record was written before CDN CNAME was ready: %+v", dns.records)
	}
}

func TestServiceApplySkipsPendingDomainVerification(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{
		domains: map[string]string{},
		verification: &DNSRecord{
			Subdomain: "_cdnauth.apps.example.com",
			Type:      "TXT",
			Value:     "verify-token",
		},
		prepareErr: errDomainVerificationPending,
	}
	dns := &fakeDNS{records: map[string]DNSRecord{}}
	store := &fakeDomainStore{domains: map[string]ManagedDomain{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(cdn.domains) != 0 {
		t.Fatalf("pending verification changed state: cdn=%+v dns=%+v store=%+v", cdn.domains, dns.records, store.domains)
	}
	if dns.records["_cdnauth.apps.example.com"].Value != "verify-token" {
		t.Fatalf("pending verification DNS record was not written: %+v", dns.records)
	}
	if store.domains["api.apps.example.com"].Verification == nil {
		t.Fatalf("pending verification was not tracked: %+v", store.domains)
	}

	err = service.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("cleanup Apply returned error: %v", err)
	}
	if _, ok := dns.records["_cdnauth.apps.example.com"]; ok {
		t.Fatalf("pending verification DNS record was not deleted: %+v", dns.records)
	}
	if _, ok := store.domains["api.apps.example.com"]; ok {
		t.Fatalf("pending frontdoor domain was not deleted: %+v", store.domains)
	}
}

func TestServiceApplyTracksCDNDomainBeforeDNSRecord(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	dns := &fakeDNS{records: map[string]DNSRecord{}, ensureErr: errors.New("dnspod unavailable")}
	store := &fakeDomainStore{domains: map[string]ManagedDomain{}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err == nil {
		t.Fatal("Apply returned nil, want DNS error")
	}
	if store.domains["api.apps.example.com"].CNAME != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("managed frontdoor domains = %+v, want CDN domain tracked before DNS write", store.domains)
	}

	dns.ensureErr = nil
	err = service.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("cleanup Apply returned error: %v", err)
	}
	if _, ok := cdn.domains["api.apps.example.com"]; ok {
		t.Fatalf("tracked CDN domain was not cleaned up: %+v", cdn.domains)
	}
	if _, ok := store.domains["api.apps.example.com"]; ok {
		t.Fatalf("tracked frontdoor domain was not deleted: %+v", store.domains)
	}
}

func TestServiceApplyDeletesPreviouslyTrackedVerificationAfterDomainReady(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	dns := &fakeDNS{records: map[string]DNSRecord{
		"_cdnauth.apps.example.com": {ID: 9, Subdomain: "_cdnauth.apps.example.com", Type: "TXT", Value: "verify-token"},
	}}
	store := &fakeDomainStore{domains: map[string]ManagedDomain{
		"api.apps.example.com": {
			Host: "api.apps.example.com",
			Verification: &DNSRecord{
				Subdomain: "_cdnauth.apps.example.com",
				Type:      "TXT",
				Value:     "verify-token",
			},
		},
	}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if _, ok := dns.records["_cdnauth.apps.example.com"]; ok {
		t.Fatalf("verification DNS record was not deleted: %+v", dns.records)
	}
	if store.domains["api.apps.example.com"].Verification != nil {
		t.Fatalf("verification was not cleared from store: %+v", store.domains)
	}
	if store.domains["api.apps.example.com"].CNAME == "" {
		t.Fatalf("CNAME was not stored after domain became ready: %+v", store.domains)
	}
}

func TestServiceApplyDeletesOnlyManagedStaleDomains(t *testing.T) {
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
	store := &fakeDomainStore{domains: map[string]ManagedDomain{
		"old.apps.example.com": {Host: "old.apps.example.com", CNAME: "old.apps.example.com.cdn.example.net"},
	}}
	service := newServiceWithClients(nil, "apps.example.com", cdn, dns, store)

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
	if _, ok := store.domains["old.apps.example.com"]; ok {
		t.Fatalf("stale managed frontdoor domain was not deleted: %+v", store.domains)
	}
	if _, ok := dns.records["other.example.com"]; !ok {
		t.Fatalf("DNS record outside base domain was deleted: %+v", dns.records)
	}
	if _, ok := cdn.domains["api.apps.example.com"]; !ok {
		t.Fatalf("unmanaged desired CDN domain was deleted: %+v", cdn.domains)
	}
	if _, ok := dns.records["peer.apps.example.com"]; !ok {
		t.Fatalf("DNS record owned by another CDN was deleted: %+v", dns.records)
	}
	if _, ok := dns.records["txt.apps.example.com"]; !ok {
		t.Fatalf("non-CNAME DNS record was deleted: %+v", dns.records)
	}
}

type fakeCDN struct {
	domains      map[string]string
	pending      map[string]bool
	verification *DNSRecord
	prepareErr   error
}

func (f *fakeCDN) PrepareDomain(ctx context.Context, _ string, dns dnsClient) (*DNSRecord, error) {
	if f.verification != nil {
		if err := dns.EnsureRecord(ctx, f.verification.Subdomain, f.verification.Type, f.verification.Value); err != nil {
			return f.verification, err
		}
	}
	return f.verification, f.prepareErr
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
	nextID    uint64
	records   map[string]DNSRecord
	ensureErr error
}

func (f *fakeDNS) ListRecords(context.Context) ([]DNSRecord, error) {
	records := make([]DNSRecord, 0, len(f.records))
	for _, record := range f.records {
		records = append(records, record)
	}
	return records, nil
}

func (f *fakeDNS) EnsureRecord(_ context.Context, host string, recordType string, value string) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
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

type fakeDomainStore struct {
	domains map[string]ManagedDomain
}

func (f *fakeDomainStore) SaveFrontDoorDomain(_ context.Context, item ManagedDomain) error {
	if f.domains == nil {
		f.domains = map[string]ManagedDomain{}
	}
	f.domains[item.Host] = item
	return nil
}

func (f *fakeDomainStore) ListFrontDoorDomains(context.Context) ([]ManagedDomain, error) {
	out := make([]ManagedDomain, 0, len(f.domains))
	for _, item := range f.domains {
		out = append(out, item)
	}
	return out, nil
}

func (f *fakeDomainStore) DeleteFrontDoorDomain(_ context.Context, host string) error {
	delete(f.domains, host)
	return nil
}
