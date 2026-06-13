package coordination

import (
	"context"
	"fmt"
	"strings"

	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

func (s *PlaneSyncer) syncFrontDoorDNS(ctx context.Context, planeID string, services []*cloudplanev1.PlaneService) error {
	if s.dns == nil {
		return nil
	}
	for _, item := range services {
		if item == nil || strings.TrimSpace(item.GetServiceId()) == "" || strings.TrimSpace(item.GetHost()) == "" {
			continue
		}
		if strings.TrimSpace(item.GetDesiredState()) == "deleted" {
			continue
		}
		if strings.TrimSpace(item.GetFrontdoorVerifySubdomain()) != "" && strings.TrimSpace(item.GetFrontdoorVerifyValue()) != "" {
			recordType := strings.TrimSpace(item.GetFrontdoorVerifyType())
			if recordType == "" {
				recordType = "TXT"
			}
			if err := s.dns.EnsureRecord(ctx, item.GetFrontdoorVerifySubdomain(), recordType, item.GetFrontdoorVerifyValue()); err != nil {
				return fmt.Errorf("ensure frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
			}
			if err := s.store.SaveServiceDNSRecord(ctx, store.DNSRecord{
				PlaneID:    planeID,
				ServiceID:  item.GetServiceId(),
				Host:       item.GetFrontdoorVerifySubdomain(),
				RecordType: recordType,
				Value:      item.GetFrontdoorVerifyValue(),
				Purpose:    store.DNSRecordPurposeFrontDoorVerification,
			}); err != nil {
				return err
			}
		}
		if strings.TrimSpace(item.GetFrontdoorCname()) == "" {
			continue
		}
		if err := s.dns.EnsureRecord(ctx, item.GetHost(), "CNAME", item.GetFrontdoorCname()); err != nil {
			return fmt.Errorf("ensure frontdoor CNAME DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
		if err := s.store.SaveServiceDNSRecord(ctx, store.DNSRecord{
			PlaneID:    planeID,
			ServiceID:  item.GetServiceId(),
			Host:       item.GetHost(),
			RecordType: "CNAME",
			Value:      item.GetFrontdoorCname(),
			Purpose:    store.DNSRecordPurposeFrontDoorCNAME,
		}); err != nil {
			return err
		}
		if err := s.deleteServiceVerificationDNS(ctx, planeID, item.GetServiceId()); err != nil {
			return fmt.Errorf("delete frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
	}
	return nil
}

func (s *PlaneSyncer) deleteServiceDNS(ctx context.Context, planeID string, serviceID string) error {
	if s.dns == nil {
		return nil
	}
	records, err := s.store.ListServiceDNSRecords(ctx, planeID, serviceID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := s.dns.DeleteRecord(ctx, record.Host, record.RecordType, record.Value); err != nil {
			return fmt.Errorf("delete DNS record %s %s for service %s: %w", record.Host, record.RecordType, serviceID, err)
		}
	}
	return s.store.DeleteServiceDNSRecords(ctx, planeID, serviceID)
}

func (s *PlaneSyncer) deleteServiceVerificationDNS(ctx context.Context, planeID string, serviceID string) error {
	records, err := s.store.ListServiceDNSRecords(ctx, planeID, serviceID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Purpose != store.DNSRecordPurposeFrontDoorVerification {
			continue
		}
		if err := s.dns.DeleteRecord(ctx, record.Host, record.RecordType, record.Value); err != nil {
			return err
		}
		if err := s.store.DeleteServiceDNSRecord(ctx, record); err != nil {
			return err
		}
	}
	return nil
}
