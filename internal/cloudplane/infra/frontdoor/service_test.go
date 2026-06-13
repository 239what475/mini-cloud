package frontdoor

import (
	"context"
	"log/slog"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestServiceSyncRoutesEnsuresDesiredDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	store := &fakeDomainStore{services: map[string]cloudmodel.Service{}}
	service := testService(cdn, store)

	err := service.SyncRoutes(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("SyncRoutes returned error: %v", err)
	}
	if cdn.domains["api.apps.example.com"] != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("CDN domains = %+v", cdn.domains)
	}
	if store.services["api.apps.example.com"].FrontDoor.CNAME != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("managed service frontdoors = %+v", store.services)
	}
}

func TestServiceSyncRoutesWaitsForCDNCNAME(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{
		domains: map[string]string{},
		pending: map[string]bool{"api.apps.example.com": true},
	}
	service := testService(cdn, &fakeDomainStore{services: map[string]cloudmodel.Service{}})

	err := service.SyncRoutes(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("SyncRoutes returned error: %v", err)
	}
	if len(cdn.domains) != 0 {
		t.Fatalf("CDN domain became ready while fake marked it pending: %+v", cdn.domains)
	}
}

func TestServiceSyncRoutesSkipsPendingDomainVerification(t *testing.T) {
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
	store := &fakeDomainStore{services: map[string]cloudmodel.Service{}}
	service := testService(cdn, store)

	err := service.SyncRoutes(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("SyncRoutes returned error: %v", err)
	}
	if len(cdn.domains) != 0 {
		t.Fatalf("pending verification changed state: cdn=%+v store=%+v", cdn.domains, store.services)
	}
	if store.services["api.apps.example.com"].FrontDoor.Verification == nil {
		t.Fatalf("pending verification was not tracked: %+v", store.services)
	}

	err = service.SyncRoutes(context.Background(), nil)
	if err != nil {
		t.Fatalf("cleanup SyncRoutes returned error: %v", err)
	}
	if store.services["api.apps.example.com"].FrontDoor.Verification != nil {
		t.Fatalf("pending frontdoor status was not cleared: %+v", store.services)
	}
}

func TestServiceSyncRoutesDeletesPreviouslyTrackedVerificationAfterDomainReady(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{}}
	store := &fakeDomainStore{services: map[string]cloudmodel.Service{
		"api.apps.example.com": {
			Host: "api.apps.example.com",
			FrontDoor: cloudmodel.FrontDoorStatus{Verification: &DNSRecord{
				Subdomain: "_cdnauth.apps.example.com",
				Type:      "TXT",
				Value:     "verify-token",
			}},
		},
	}}
	service := testService(cdn, store)

	err := service.SyncRoutes(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("SyncRoutes returned error: %v", err)
	}
	if store.services["api.apps.example.com"].FrontDoor.Verification != nil {
		t.Fatalf("verification was not cleared from store: %+v", store.services)
	}
	if store.services["api.apps.example.com"].FrontDoor.CNAME == "" {
		t.Fatalf("CNAME was not stored after domain became ready: %+v", store.services)
	}
}

func TestServiceSyncRoutesDeletesOnlyManagedStaleDomains(t *testing.T) {
	t.Parallel()

	cdn := &fakeCDN{domains: map[string]string{
		"api.apps.example.com": "api.apps.example.com.cdn.example.net",
		"old.apps.example.com": "old.apps.example.com.cdn.example.net",
		"other.example.com":    "other.example.com.cdn.example.net",
	}}
	store := &fakeDomainStore{services: map[string]cloudmodel.Service{
		"old.apps.example.com": {Host: "old.apps.example.com", FrontDoor: cloudmodel.FrontDoorStatus{CNAME: "old.apps.example.com.cdn.example.net"}},
	}}
	service := testService(cdn, store)

	err := service.SyncRoutes(context.Background(), []cloudmodel.Route{{Host: "api.apps.example.com"}})
	if err != nil {
		t.Fatalf("SyncRoutes returned error: %v", err)
	}
	if _, ok := cdn.domains["old.apps.example.com"]; ok {
		t.Fatalf("stale CDN domain was not deleted: %+v", cdn.domains)
	}
	if _, ok := cdn.domains["other.example.com"]; !ok {
		t.Fatalf("CDN domain outside base domain was deleted: %+v", cdn.domains)
	}
	if store.services["old.apps.example.com"].FrontDoor.CNAME != "" {
		t.Fatalf("stale managed frontdoor status was not cleared: %+v", store.services)
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
	services map[string]cloudmodel.Service
}

func (f *fakeDomainStore) SaveServiceFrontDoor(_ context.Context, host string, status FrontDoorStatus) error {
	if f.services == nil {
		f.services = map[string]cloudmodel.Service{}
	}
	item := f.services[host]
	item.Host = host
	item.FrontDoor = status
	f.services[host] = item
	return nil
}

func (f *fakeDomainStore) ListServiceFrontDoors(context.Context) ([]cloudmodel.Service, error) {
	out := make([]cloudmodel.Service, 0, len(f.services))
	for _, item := range f.services {
		if item.FrontDoor.CNAME == "" && item.FrontDoor.Verification == nil {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (f *fakeDomainStore) ClearServiceFrontDoor(_ context.Context, host string) error {
	item := f.services[host]
	item.Host = host
	item.FrontDoor = cloudmodel.FrontDoorStatus{}
	f.services[host] = item
	return nil
}
