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
	ServiceID  string
	Host       string
	RecordType string
	Value      string
	Purpose    string
}

func (s *Store) SaveServiceDNSRecord(ctx context.Context, record DNSRecord) error {
	record = normalizeDNSRecord(record)
	if record.ServiceID == "" || record.Host == "" || record.RecordType == "" || record.Purpose == "" {
		return invalidInput(fmt.Errorf("service DNS record requires serviceID, host, recordType and purpose"))
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO service_dns_records (
			service_id,
			host,
			record_type,
			value,
			purpose
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (service_id, host, record_type, purpose) DO UPDATE
		SET value = EXCLUDED.value,
		    updated_at = now()
	`, record.ServiceID, record.Host, record.RecordType, record.Value, record.Purpose)
	if err != nil {
		return fmt.Errorf("save service DNS record: %w", err)
	}
	return nil
}

func (s *Store) DeleteServiceDNSRecord(ctx context.Context, record DNSRecord) error {
	record = normalizeDNSRecord(record)
	if record.ServiceID == "" || record.Host == "" || record.RecordType == "" || record.Purpose == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM service_dns_records
		WHERE service_id = $1
		  AND host = $2
		  AND record_type = $3
		  AND purpose = $4
	`, record.ServiceID, record.Host, record.RecordType, record.Purpose); err != nil {
		return fmt.Errorf("delete service DNS record: %w", err)
	}
	return nil
}

func (s *Store) ListServiceDNSRecords(ctx context.Context, serviceID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service_id, host, record_type, value, purpose
		FROM service_dns_records
		WHERE service_id = $1
		ORDER BY created_at ASC, host ASC, record_type ASC, purpose ASC
	`, strings.TrimSpace(serviceID))
	if err != nil {
		return nil, fmt.Errorf("query service DNS records: %w", err)
	}
	defer rows.Close()

	items := make([]DNSRecord, 0)
	for rows.Next() {
		var item DNSRecord
		if err := rows.Scan(&item.ServiceID, &item.Host, &item.RecordType, &item.Value, &item.Purpose); err != nil {
			return nil, fmt.Errorf("scan service DNS record: %w", err)
		}
		items = append(items, normalizeDNSRecord(item))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service DNS records: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteServiceDNSRecords(ctx context.Context, serviceID string) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM service_dns_records
		WHERE service_id = $1
	`, strings.TrimSpace(serviceID)); err != nil {
		return fmt.Errorf("delete service DNS records: %w", err)
	}
	return nil
}

func normalizeDNSRecord(record DNSRecord) DNSRecord {
	return DNSRecord{
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
