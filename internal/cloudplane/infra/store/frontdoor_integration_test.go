package store_test

import (
	"context"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationFrontDoorDomainLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	if err := db.Store.SaveFrontDoorDomain(ctx, cloudmodel.ManagedFrontDoorDomain{
		Host:  "API.Apps.Example.com.",
		CNAME: "api.apps.example.com.cdn.example.net.",
		Verification: &cloudmodel.FrontDoorDNSRecord{
			Subdomain: "_cdnauth.apps.example.com.",
			Type:      "TXT",
			Value:     "verify-token",
		},
	}); err != nil {
		t.Fatalf("SaveFrontDoorDomain returned error: %v", err)
	}
	items, err := db.Store.ListFrontDoorDomains(ctx)
	if err != nil {
		t.Fatalf("ListFrontDoorDomains returned error: %v", err)
	}
	if len(items) != 1 || items[0].Host != "api.apps.example.com" || items[0].CNAME != "api.apps.example.com.cdn.example.net" {
		t.Fatalf("frontdoor domains = %+v", items)
	}
	if items[0].Verification == nil || items[0].Verification.Subdomain != "_cdnauth.apps.example.com" || items[0].Verification.Type != "TXT" || items[0].Verification.Value != "verify-token" {
		t.Fatalf("frontdoor verification = %+v", items[0].Verification)
	}

	if err := db.Store.SaveFrontDoorDomain(ctx, cloudmodel.ManagedFrontDoorDomain{Host: "api.apps.example.com", CNAME: "api.apps.example.com.next-cdn.example.net"}); err != nil {
		t.Fatalf("second SaveFrontDoorDomain returned error: %v", err)
	}
	items, err = db.Store.ListFrontDoorDomains(ctx)
	if err != nil {
		t.Fatalf("second ListFrontDoorDomains returned error: %v", err)
	}
	if len(items) != 1 || items[0].CNAME != "api.apps.example.com.next-cdn.example.net" || items[0].Verification != nil {
		t.Fatalf("frontdoor domains after update = %+v", items)
	}

	if err := db.Store.DeleteFrontDoorDomain(ctx, "api.apps.example.com"); err != nil {
		t.Fatalf("DeleteFrontDoorDomain returned error: %v", err)
	}
	items, err = db.Store.ListFrontDoorDomains(ctx)
	if err != nil {
		t.Fatalf("third ListFrontDoorDomains returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("frontdoor domains after delete = %+v", items)
	}
}
