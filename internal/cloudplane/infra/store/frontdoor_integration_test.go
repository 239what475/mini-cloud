package store_test

import (
	"context"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationServiceFrontDoorLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-frontdoor",
		Name:       "api",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})
	if err := db.Store.SaveServiceFrontDoor(ctx, "API.Apps.Example.Test.", cloudmodel.FrontDoorStatus{
		CNAME: "api.apps.example.test.cdn.example.net.",
		Verification: &cloudmodel.FrontDoorDNSRecord{
			Subdomain: "_cdnauth.apps.example.test.",
			Type:      "TXT",
			Value:     "verify-token",
		},
	}); err != nil {
		t.Fatalf("SaveServiceFrontDoor returned error: %v", err)
	}
	items, err := db.Store.ListServiceFrontDoors(ctx)
	if err != nil {
		t.Fatalf("ListServiceFrontDoors returned error: %v", err)
	}
	if len(items) != 1 || items[0].Host != "api.apps.example.test" || items[0].FrontDoor.CNAME != "api.apps.example.test.cdn.example.net" {
		t.Fatalf("service frontdoors = %+v", items)
	}
	verification := items[0].FrontDoor.Verification
	if verification == nil || verification.Subdomain != "_cdnauth.apps.example.test" || verification.Type != "TXT" || verification.Value != "verify-token" {
		t.Fatalf("frontdoor verification = %+v", verification)
	}

	if err := db.Store.SaveServiceFrontDoor(ctx, "api.apps.example.test", cloudmodel.FrontDoorStatus{CNAME: "api.apps.example.test.next-cdn.example.net"}); err != nil {
		t.Fatalf("second SaveServiceFrontDoor returned error: %v", err)
	}
	items, err = db.Store.ListServiceFrontDoors(ctx)
	if err != nil {
		t.Fatalf("second ListServiceFrontDoors returned error: %v", err)
	}
	if len(items) != 1 || items[0].FrontDoor.CNAME != "api.apps.example.test.next-cdn.example.net" || items[0].FrontDoor.Verification != nil {
		t.Fatalf("service frontdoor after update = %+v", items)
	}

	if err := db.Store.ClearServiceFrontDoor(ctx, "api.apps.example.test"); err != nil {
		t.Fatalf("ClearServiceFrontDoor returned error: %v", err)
	}
	items, err = db.Store.ListServiceFrontDoors(ctx)
	if err != nil {
		t.Fatalf("third ListServiceFrontDoors returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("service frontdoors after clear = %+v", items)
	}
}
