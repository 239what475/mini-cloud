package coordination

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

func (s *PlaneSyncer) syncFrontDoorDNS(ctx context.Context, planeID string, domains []*cloudplanev1.PlaneFrontDoorDomain) error {
	if s.dns == nil {
		return nil
	}
	for _, item := range domains {
		if item == nil || strings.TrimSpace(item.GetHost()) == "" {
			continue
		}
		serviceItem, err := s.serviceForFrontDoorDomain(ctx, planeID, item.GetHost())
		if err != nil {
			if errors.Is(err, store.ErrServiceNotFound) {
				continue
			}
			return err
		}
		if serviceItem.Status.DesiredState == model.DesiredStateDeleted {
			continue
		}
		if strings.TrimSpace(item.GetVerifySubdomain()) != "" && strings.TrimSpace(item.GetVerifyValue()) != "" {
			recordType := strings.TrimSpace(item.GetVerifyType())
			if recordType == "" {
				recordType = "TXT"
			}
			if err := s.dns.EnsureRecord(ctx, item.GetVerifySubdomain(), recordType, item.GetVerifyValue()); err != nil {
				return fmt.Errorf("ensure frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
			}
			if err := s.store.SaveServiceDNSRecord(ctx, store.DNSRecord{
				ServiceID:  serviceItem.Metadata.ID,
				Host:       item.GetVerifySubdomain(),
				RecordType: recordType,
				Value:      item.GetVerifyValue(),
				Purpose:    store.DNSRecordPurposeFrontDoorVerification,
			}); err != nil {
				return err
			}
		}
		if strings.TrimSpace(item.GetCname()) == "" {
			continue
		}
		if err := s.dns.EnsureRecord(ctx, item.GetHost(), "CNAME", item.GetCname()); err != nil {
			return fmt.Errorf("ensure frontdoor CNAME DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
		if err := s.store.SaveServiceDNSRecord(ctx, store.DNSRecord{
			ServiceID:  serviceItem.Metadata.ID,
			Host:       item.GetHost(),
			RecordType: "CNAME",
			Value:      item.GetCname(),
			Purpose:    store.DNSRecordPurposeFrontDoorCNAME,
		}); err != nil {
			return err
		}
		if err := s.deleteServiceVerificationDNS(ctx, serviceItem); err != nil {
			return fmt.Errorf("delete frontdoor verification DNS for plane %s host %s: %w", planeID, item.GetHost(), err)
		}
	}
	return nil
}

func (s *PlaneSyncer) serviceForFrontDoorDomain(ctx context.Context, planeID string, host string) (model.Service, error) {
	serviceItem, err := s.store.GetServiceByHost(ctx, host)
	if err != nil {
		return model.Service{}, err
	}
	if strings.TrimSpace(serviceItem.Spec.PlaneID) != planeID {
		return model.Service{}, store.ErrServiceNotFound
	}
	return serviceItem, nil
}

func (s *PlaneSyncer) deleteServiceDNS(ctx context.Context, serviceItem model.Service) error {
	if s.dns == nil {
		return nil
	}
	records, err := s.store.ListServiceDNSRecords(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := s.dns.DeleteRecord(ctx, record.Host, record.RecordType, record.Value); err != nil {
			return fmt.Errorf("delete DNS record %s %s for service %s: %w", record.Host, record.RecordType, serviceItem.Metadata.ID, err)
		}
	}
	return s.store.DeleteServiceDNSRecords(ctx, serviceItem.Metadata.ID)
}

func (s *PlaneSyncer) deleteServiceVerificationDNS(ctx context.Context, serviceItem model.Service) error {
	records, err := s.store.ListServiceDNSRecords(ctx, serviceItem.Metadata.ID)
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
