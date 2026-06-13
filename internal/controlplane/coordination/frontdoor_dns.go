package coordination

import (
	"context"
	"fmt"
	"strings"

	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

const (
	dnsRecordPurposeFrontDoorVerification = "frontdoor_verification"
	dnsRecordPurposeFrontDoorCNAME        = "frontdoor_cname"
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
			record := managedDNSRecord{
				PlaneID:    planeID,
				ServiceID:  item.GetServiceId(),
				Host:       item.GetFrontdoorVerifySubdomain(),
				RecordType: recordType,
				Value:      item.GetFrontdoorVerifyValue(),
				Purpose:    dnsRecordPurposeFrontDoorVerification,
			}
			if err := s.dns.EnsureRecord(ctx, record); err != nil {
				return fmt.Errorf("ensure frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
			}
		}
		if strings.TrimSpace(item.GetFrontdoorCname()) == "" {
			continue
		}
		record := managedDNSRecord{
			PlaneID:    planeID,
			ServiceID:  item.GetServiceId(),
			Host:       item.GetHost(),
			RecordType: "CNAME",
			Value:      item.GetFrontdoorCname(),
			Purpose:    dnsRecordPurposeFrontDoorCNAME,
		}
		if err := s.dns.EnsureRecord(ctx, record); err != nil {
			return fmt.Errorf("ensure frontdoor CNAME DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
		if strings.TrimSpace(item.GetFrontdoorVerifySubdomain()) != "" && strings.TrimSpace(item.GetFrontdoorVerifyValue()) != "" {
			recordType := strings.TrimSpace(item.GetFrontdoorVerifyType())
			if recordType == "" {
				recordType = "TXT"
			}
			record := managedDNSRecord{
				PlaneID:    planeID,
				ServiceID:  item.GetServiceId(),
				Host:       item.GetFrontdoorVerifySubdomain(),
				RecordType: recordType,
				Value:      item.GetFrontdoorVerifyValue(),
				Purpose:    dnsRecordPurposeFrontDoorVerification,
			}
			if err := s.dns.DeleteRecord(ctx, record); err != nil {
				return fmt.Errorf("delete frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
			}
		} else if err := s.dns.DeleteServiceRecords(ctx, planeID, item.GetServiceId(), dnsRecordPurposeFrontDoorVerification); err != nil {
			return fmt.Errorf("delete frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
	}
	return nil
}

func (s *PlaneSyncer) deleteServiceDNS(ctx context.Context, planeID string, serviceID string) error {
	if s.dns == nil {
		return nil
	}
	return s.dns.DeleteServiceRecords(ctx, planeID, serviceID, "")
}
