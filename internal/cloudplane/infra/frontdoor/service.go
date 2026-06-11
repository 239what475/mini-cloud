package frontdoor

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

type cdnClient interface {
	ListDomains(context.Context, string) ([]CDNDomain, error)
	EnsureDomain(context.Context, string) (string, error)
	DeleteDomain(context.Context, string) error
}

type dnsClient interface {
	ListRecords(context.Context) ([]DNSRecord, error)
	EnsureCNAME(context.Context, string, string) error
	DeleteRecord(context.Context, DNSRecord) error
}

type CDNDomain struct {
	Host  string
	CNAME string
}

type DNSRecord struct {
	ID        uint64
	Subdomain string
	Type      string
	Value     string
}

type Service struct {
	logger     *slog.Logger
	baseDomain string
	cdn        cdnClient
	dns        dnsClient
}

func NewService(logger *slog.Logger, cfg cloudplaneconfig.Config) (*Service, error) {
	if !cfg.Ingress.FrontDoor.Enabled {
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
	return newServiceWithClients(logger, cfg.Ingress.BaseDomain, cdn, dns), nil
}

func newServiceWithClients(logger *slog.Logger, baseDomain string, cdn cdnClient, dns dnsClient) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:     logger,
		baseDomain: cleanDomain(baseDomain),
		cdn:        cdn,
		dns:        dns,
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

	for _, host := range sortedHosts(desired) {
		cname, err := s.cdn.EnsureDomain(ctx, host)
		if err != nil {
			return fmt.Errorf("ensure CDN domain %s: %w", host, err)
		}
		if strings.TrimSpace(cname) == "" {
			return fmt.Errorf("CDN domain %s has empty CNAME", host)
		}
		if err := s.dns.EnsureCNAME(ctx, host, cname); err != nil {
			return fmt.Errorf("ensure DNS record %s: %w", host, err)
		}
	}

	records, err := s.dns.ListRecords(ctx)
	if err != nil {
		return fmt.Errorf("list DNS records: %w", err)
	}
	for _, record := range records {
		host := cleanDomain(record.Subdomain)
		if record.Type != "CNAME" || !domainIsUnder(host, s.baseDomain) {
			continue
		}
		if _, ok := desired[host]; ok {
			continue
		}
		if err := s.dns.DeleteRecord(ctx, record); err != nil {
			return fmt.Errorf("delete stale DNS record %s: %w", host, err)
		}
	}

	cdnDomains, err := s.cdn.ListDomains(ctx, s.baseDomain)
	if err != nil {
		return fmt.Errorf("list CDN domains: %w", err)
	}
	for _, domain := range cdnDomains {
		host := cleanDomain(domain.Host)
		if _, ok := desired[host]; ok || !domainIsUnder(host, s.baseDomain) {
			continue
		}
		if err := s.cdn.DeleteDomain(ctx, host); err != nil {
			return fmt.Errorf("delete stale CDN domain %s: %w", host, err)
		}
	}

	s.logger.Debug("cloud-plane reconciled frontdoor", "routes", len(desired))
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
