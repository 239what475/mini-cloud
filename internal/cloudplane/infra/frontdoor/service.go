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
	PrepareDomain(context.Context, string) (*DNSRecord, error)
	EnsureDomain(context.Context, string) (string, error)
	DeleteDomain(context.Context, string) error
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
	store      domainStore
}

func NewService(logger *slog.Logger, cfg cloudplaneconfig.Config, stores domainStore) (*Service, error) {
	if strings.TrimSpace(cfg.Ingress.PublicOrigin) == "" {
		return nil, nil
	}
	cdn, err := newCDNClient(cfg)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:     logger,
		baseDomain: cleanDomain(cfg.Ingress.BaseDomain),
		cdn:        cdn,
		store:      stores,
	}, nil
}

func newCDNClient(cfg cloudplaneconfig.Config) (cdnClient, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Infrastructure.Provider)) {
	case "aliyun":
		return newAliyunCDNClient(cfg)
	case "tencent":
		return newTencentCDNClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported frontdoor CDN provider %q", cfg.Infrastructure.Provider)
	}
}

func (s *Service) SyncRoutes(ctx context.Context, routes []cloudmodel.Route) error {
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
	if s.store != nil {
		items, err := s.store.ListFrontDoorDomains(ctx)
		if err != nil {
			return err
		}
		managed = items
	}

	for _, host := range sortedHosts(desired) {
		verification, err := s.cdn.PrepareDomain(ctx, host)
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

func cleanDomainPointer(value *string) string {
	if value == nil {
		return ""
	}
	return cleanDomain(*value)
}

func domainIsUnder(child string, parent string) bool {
	child = cleanDomain(child)
	parent = cleanDomain(parent)
	return child == parent || strings.HasSuffix(child, "."+parent)
}
