package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

var errDomainVerificationPending = errors.New("CDN domain verification is pending")

type cdnClient interface {
	PrepareDomain(context.Context, string, dnsClient) (*DNSRecord, error)
	EnsureDomain(context.Context, string) (string, error)
	DeleteDomain(context.Context, string) error
	OwnsCNAME(string) bool
}

type dnsClient interface {
	ListRecords(context.Context) ([]DNSRecord, error)
	EnsureRecord(context.Context, string, string, string) error
	DeleteRecord(context.Context, DNSRecord) error
}

type domainStore interface {
	SaveFrontDoorDomain(context.Context, ManagedDomain) error
	ListFrontDoorDomains(context.Context) ([]ManagedDomain, error)
	DeleteFrontDoorDomain(context.Context, string) error
}

type cdnDomain struct {
	Exists bool
	CNAME  string
}

type DNSRecord = cloudmodel.FrontDoorDNSRecord
type ManagedDomain = cloudmodel.ManagedFrontDoorDomain

type Service struct {
	logger     *slog.Logger
	baseDomain string
	cdn        cdnClient
	dns        dnsClient
	store      domainStore
}

func NewService(logger *slog.Logger, cfg cloudplaneconfig.Config, stores domainStore) (*Service, error) {
	if strings.TrimSpace(cfg.Ingress.FrontDoor.DNSPodDomain) == "" {
		return nil, nil
	}
	cdn, err := newCDNClient(cfg)
	if err != nil {
		return nil, err
	}
	dns, err := newDNSPodClient(cfg.Ingress.FrontDoor)
	if err != nil {
		return nil, err
	}
	return newServiceWithClients(logger, cfg.Ingress.BaseDomain, cdn, dns, stores), nil
}

func newServiceWithClients(logger *slog.Logger, baseDomain string, cdn cdnClient, dns dnsClient, stores domainStore) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:     logger,
		baseDomain: cleanDomain(baseDomain),
		cdn:        cdn,
		dns:        dns,
		store:      stores,
	}
}

func (s *Service) Apply(ctx context.Context, routes []cloudmodel.Route) error {
	desired := make(map[string]struct{})
	for _, route := range routes {
		host := cleanDomain(route.Host)
		if host == "" {
			continue
		}
		if !domainIsUnder(host, s.baseDomain) {
			return fmt.Errorf("frontdoor route host %q is outside base domain %q", host, s.baseDomain)
		}
		desired[host] = struct{}{}
	}

	var managed []ManagedDomain
	managedByHost := map[string]ManagedDomain{}
	if s.store != nil {
		items, err := s.store.ListFrontDoorDomains(ctx)
		if err != nil {
			return err
		}
		managed = items
		for _, item := range items {
			managedByHost[cleanDomain(item.Host)] = item
		}
	}

	for _, host := range sortedHosts(desired) {
		verification, err := s.cdn.PrepareDomain(ctx, host, s.dns)
		if s.store != nil && verification != nil {
			if saveErr := s.store.SaveFrontDoorDomain(ctx, ManagedDomain{Host: host, Verification: verification}); saveErr != nil {
				return saveErr
			}
		}
		if err != nil {
			if errors.Is(err, errDomainVerificationPending) {
				s.logger.Debug("frontdoor CDN domain verification is still pending", "host", host)
				continue
			}
			return fmt.Errorf("prepare CDN domain %s: %w", host, err)
		}
		cname, err := s.cdn.EnsureDomain(ctx, host)
		if err != nil {
			return fmt.Errorf("ensure CDN domain %s: %w", host, err)
		}
		if strings.TrimSpace(cname) == "" {
			s.logger.Debug("frontdoor CDN domain is still configuring", "host", host)
			continue
		}
		if s.store != nil {
			if err := s.store.SaveFrontDoorDomain(ctx, ManagedDomain{Host: host, CNAME: cname, Verification: verification}); err != nil {
				return err
			}
		}
		if err := s.dns.EnsureRecord(ctx, host, "CNAME", cname); err != nil {
			return fmt.Errorf("ensure DNS record %s: %w", host, err)
		}
		verificationToDelete := verification
		if verificationToDelete == nil && managedByHost[host].Verification != nil {
			verificationToDelete = managedByHost[host].Verification
		}
		if verificationToDelete != nil {
			if err := s.deleteDNSRecordIfPresent(ctx, *verificationToDelete); err != nil {
				return fmt.Errorf("delete CDN verification DNS record %s: %w", verificationToDelete.Subdomain, err)
			}
		}
		if s.store != nil {
			if err := s.store.SaveFrontDoorDomain(ctx, ManagedDomain{Host: host, CNAME: cname}); err != nil {
				return err
			}
		}
	}

	if s.store == nil {
		s.logger.Debug("cloud-plane reconciled frontdoor", "routes", len(desired))
		return nil
	}
	if len(managed) == 0 {
		s.logger.Debug("cloud-plane reconciled frontdoor", "routes", len(desired))
		return nil
	}

	records, err := s.dns.ListRecords(ctx)
	if err != nil {
		return fmt.Errorf("list DNS records: %w", err)
	}
	recordsByHost := map[string]DNSRecord{}
	for _, record := range records {
		if record.Type == "CNAME" && s.cdn.OwnsCNAME(record.Value) {
			recordsByHost[cleanDomain(record.Subdomain)] = record
		}
	}

	for _, domain := range managed {
		host := cleanDomain(domain.Host)
		if !domainIsUnder(host, s.baseDomain) {
			if err := s.store.DeleteFrontDoorDomain(ctx, host); err != nil {
				return err
			}
			continue
		}
		if _, ok := desired[host]; ok {
			continue
		}
		if record, ok := recordsByHost[host]; ok {
			if err := s.dns.DeleteRecord(ctx, record); err != nil {
				return fmt.Errorf("delete stale DNS record %s: %w", host, err)
			}
		}
		if domain.Verification != nil {
			if err := s.deleteDNSRecordFromSnapshot(ctx, records, *domain.Verification); err != nil {
				return fmt.Errorf("delete CDN verification DNS record %s: %w", domain.Verification.Subdomain, err)
			}
		}
		if err := s.cdn.DeleteDomain(ctx, host); err != nil {
			return fmt.Errorf("delete stale CDN domain %s: %w", host, err)
		}
		if err := s.store.DeleteFrontDoorDomain(ctx, host); err != nil {
			return err
		}
	}

	s.logger.Debug("cloud-plane reconciled frontdoor", "routes", len(desired))
	return nil
}

func (s *Service) deleteDNSRecordIfPresent(ctx context.Context, desired DNSRecord) error {
	records, err := s.dns.ListRecords(ctx)
	if err != nil {
		return err
	}
	return s.deleteDNSRecordFromSnapshot(ctx, records, desired)
}

func (s *Service) deleteDNSRecordFromSnapshot(ctx context.Context, records []DNSRecord, desired DNSRecord) error {
	desired.Subdomain = cleanDomain(desired.Subdomain)
	desired.Type = cleanRecordType(desired.Type)
	desired.Value = cleanRecordValue(&desired.Value, desired.Type)
	for _, record := range records {
		if cleanDomain(record.Subdomain) != desired.Subdomain {
			continue
		}
		if cleanRecordType(record.Type) != desired.Type {
			continue
		}
		if cleanRecordValue(&record.Value, record.Type) != desired.Value {
			continue
		}
		return s.dns.DeleteRecord(ctx, record)
	}
	return nil
}

func sortedHosts(values map[string]struct{}) []string {
	hosts := make([]string, 0, len(values))
	for host := range values {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func cleanDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func domainIsUnder(child string, parent string) bool {
	child = cleanDomain(child)
	parent = cleanDomain(parent)
	return child == parent || strings.HasSuffix(child, "."+parent)
}
