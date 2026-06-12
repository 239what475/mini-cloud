package frontdoor

import (
	"context"
	"log/slog"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestServiceApplyEnsuresDesiredDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	store := &fakeDomainStore{domains: map[string]ManagedDomain{}}
	service := testService(cdn, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if cdn.domains["api.apps.example.com"] != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("CDN domains = %+v", cdn.domains)
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
	service := testService(cdn, &fakeDomainStore{domains: map[string]ManagedDomain{}})

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(cdn.domains) != 0 {
		t.Fatalf("CDN domain became ready while fake marked it pending: %+v", cdn.domains)
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
	store := &fakeDomainStore{domains: map[string]ManagedDomain{}}
	service := testService(cdn, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(cdn.domains) != 0 {
		t.Fatalf("pending verification changed state: cdn=%+v store=%+v", cdn.domains, store.domains)
	}
	if store.domains["api.apps.example.com"].Verification == nil {
		t.Fatalf("pending verification was not tracked: %+v", store.domains)
	}

	err = service.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("cleanup Apply returned error: %v", err)
	}
	if _, ok := store.domains["api.apps.example.com"]; ok {
		t.Fatalf("pending frontdoor domain was not deleted: %+v", store.domains)
	}
}

func TestServiceApplyDeletesPreviouslyTrackedVerificationAfterDomainReady(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
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
	service := testService(cdn, store)

	err := service.Apply(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
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
	store := &fakeDomainStore{domains: map[string]ManagedDomain{
		"old.apps.example.com": {Host: "old.apps.example.com", CNAME: "old.apps.example.com.cdn.example.net"},
	}}
	service := testService(cdn, store)

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
	if _, ok := store.domains["old.apps.example.com"]; ok {
		t.Fatalf("stale managed frontdoor domain was not deleted: %+v", store.domains)
	}
	if _, ok := cdn.domains["api.apps.example.com"]; !ok {
		t.Fatalf("unmanaged desired CDN domain was deleted: %+v", cdn.domains)
	}
}

func testService(cdn cdnClient, store domainStore) *Service {
	return &Service{
		logger:     slog.Default(),
		baseDomain: "apps.example.com",
		cdn:        cdn,
		store:      store,
	}
}

type fakeCDN struct {
	domains      map[string]string
	pending      map[string]bool
	verification *DNSRecord
	prepareErr   error
}

func (f *fakeCDN) PrepareDomain(context.Context, string) (*DNSRecord, error) {
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
