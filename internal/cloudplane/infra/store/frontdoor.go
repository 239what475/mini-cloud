package store

import (
	"context"
	"fmt"
	"strings"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func (s *Store) SaveFrontDoorDomain(ctx context.Context, item cloudmodel.ManagedFrontDoorDomain) error {
	host := cleanDomain(item.Host)
	cname := cleanDomain(item.CNAME)
	if host == "" {
		return nil
	}
	verifySubdomain := ""
	verifyType := ""
	verifyValue := ""
	if item.Verification != nil {
		verifySubdomain = cleanDomain(item.Verification.Subdomain)
		verifyType = strings.ToUpper(strings.TrimSpace(item.Verification.Type))
		verifyValue = strings.TrimSpace(item.Verification.Value)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO frontdoor_domains (host, cname, verify_subdomain, verify_type, verify_value)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (host) DO UPDATE
		SET cname = EXCLUDED.cname,
		    verify_subdomain = EXCLUDED.verify_subdomain,
		    verify_type = EXCLUDED.verify_type,
		    verify_value = EXCLUDED.verify_value,
		    updated_at = now()
	`, host, cname, verifySubdomain, verifyType, verifyValue)
	if err != nil {
		return fmt.Errorf("save frontdoor domain: %w", err)
	}
	return nil
}

func (s *Store) ListFrontDoorDomains(ctx context.Context) ([]cloudmodel.ManagedFrontDoorDomain, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT host, cname, verify_subdomain, verify_type, verify_value
		FROM frontdoor_domains
		ORDER BY host ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query frontdoor domains: %w", err)
	}
	defer rows.Close()

	var items []cloudmodel.ManagedFrontDoorDomain
	for rows.Next() {
		item := cloudmodel.ManagedFrontDoorDomain{}
		var verifySubdomain string
		var verifyType string
		var verifyValue string
		if err := rows.Scan(&item.Host, &item.CNAME, &verifySubdomain, &verifyType, &verifyValue); err != nil {
			return nil, fmt.Errorf("scan frontdoor domain: %w", err)
		}
		item.Host = cleanDomain(item.Host)
		item.CNAME = cleanDomain(item.CNAME)
		if verifySubdomain != "" && verifyType != "" && verifyValue != "" {
			item.Verification = &cloudmodel.FrontDoorDNSRecord{
				Subdomain: cleanDomain(verifySubdomain),
				Type:      strings.ToUpper(strings.TrimSpace(verifyType)),
				Value:     strings.TrimSpace(verifyValue),
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate frontdoor domains: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteFrontDoorDomain(ctx context.Context, host string) error {
	host = cleanDomain(host)
	if host == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM frontdoor_domains WHERE host = $1`, host); err != nil {
		return fmt.Errorf("delete frontdoor domain: %w", err)
	}
	return nil
}

func cleanDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}
