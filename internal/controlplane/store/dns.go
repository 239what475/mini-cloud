package store

import (
	"context"
	"fmt"
	"strings"
)

const (
	DNSRecordPurposeFrontDoorCNAME        = "frontdoor-cname"
	DNSRecordPurposeFrontDoorVerification = "frontdoor-verification"
)

type DNSRecord struct {
	PlaneID    string
	ServiceID  string
	Host       string
	RecordType string
	Value      string
	Purpose    string
}

func (s *Store) SaveServiceDNSRecord(ctx context.Context, record DNSRecord) error {
	record = normalizeDNSRecord(record)
	if record.PlaneID == "" || record.ServiceID == "" || record.Host == "" || record.RecordType == "" || record.Purpose == "" {
		return invalidInput(fmt.Errorf("service DNS record requires planeID, serviceID, host, recordType and purpose"))
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO service_dns_records (
			plane_id,
			service_id,
			host,
			record_type,
			value,
			purpose
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (plane_id, service_id, host, record_type, purpose) DO UPDATE
		SET value = EXCLUDED.value,
		    updated_at = now()
	`, record.PlaneID, record.ServiceID, record.Host, record.RecordType, record.Value, record.Purpose)
	if err != nil {
		return fmt.Errorf("save service DNS record: %w", err)
	}
	return nil
}

func (s *Store) DeleteServiceDNSRecord(ctx context.Context, record DNSRecord) error {
	record = normalizeDNSRecord(record)
	if record.PlaneID == "" || record.ServiceID == "" || record.Host == "" || record.RecordType == "" || record.Purpose == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM service_dns_records
		WHERE plane_id = $1
		  AND service_id = $2
		  AND host = $3
		  AND record_type = $4
		  AND purpose = $5
	`, record.PlaneID, record.ServiceID, record.Host, record.RecordType, record.Purpose); err != nil {
		return fmt.Errorf("delete service DNS record: %w", err)
	}
	return nil
}

func (s *Store) ListServiceDNSRecords(ctx context.Context, planeID string, serviceID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plane_id, service_id, host, record_type, value, purpose
		FROM service_dns_records
		WHERE plane_id = $1
		  AND service_id = $2
		ORDER BY created_at ASC, host ASC, record_type ASC, purpose ASC
	`, strings.TrimSpace(planeID), strings.TrimSpace(serviceID))
	if err != nil {
		return nil, fmt.Errorf("query service DNS records: %w", err)
	}
	defer rows.Close()

	items := make([]DNSRecord, 0)
	for rows.Next() {
		var item DNSRecord
		if err := rows.Scan(&item.PlaneID, &item.ServiceID, &item.Host, &item.RecordType, &item.Value, &item.Purpose); err != nil {
			return nil, fmt.Errorf("scan service DNS record: %w", err)
		}
		items = append(items, normalizeDNSRecord(item))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service DNS records: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteServiceDNSRecords(ctx context.Context, planeID string, serviceID string) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM service_dns_records
		WHERE plane_id = $1
		  AND service_id = $2
	`, strings.TrimSpace(planeID), strings.TrimSpace(serviceID)); err != nil {
		return fmt.Errorf("delete service DNS records: %w", err)
	}
	return nil
}

func normalizeDNSRecord(record DNSRecord) DNSRecord {
	return DNSRecord{
		PlaneID:    strings.TrimSpace(record.PlaneID),
		ServiceID:  strings.TrimSpace(record.ServiceID),
		Host:       cleanServiceDomain(record.Host),
		RecordType: strings.ToUpper(strings.TrimSpace(record.RecordType)),
		Value:      cleanServiceDNSValue(record.RecordType, record.Value),
		Purpose:    strings.TrimSpace(record.Purpose),
	}
}

func cleanServiceDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func cleanServiceDNSValue(recordType string, value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(recordType, "CNAME") {
		return cleanServiceDomain(value)
	}
	return value
}
