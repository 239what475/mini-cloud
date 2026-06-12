package controlplane

import (
	"context"
	"log/slog"
	"testing"

	"mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"
	"mini-cloud/internal/transport"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestDeleteServiceReturnsNotFoundForMissingService(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(transport.BearerMetadataKey, transport.BearerHeader("southbound-token")))
	db := testutil.OpenCloudPlaneTestDatabase(t)
	server := newServiceServer(slog.Default(), db.Store, newAuthenticator("southbound-token"))

	_, err := server.DeleteService(ctx, &cloudplanev1.DeleteServiceRequest{
		ServiceId:         "missing-service",
		ServiceGeneration: 1,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("DeleteService error = %v, want NotFound", err)
	}
}

func TestUpsertServiceValidatesServiceSpec(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(transport.BearerMetadataKey, transport.BearerHeader("southbound-token")))
	db := testutil.OpenCloudPlaneTestDatabase(t)
	server := newServiceServer(slog.Default(), db.Store, newAuthenticator("southbound-token"))

	_, err := server.UpsertService(ctx, &cloudplanev1.UpsertServiceRequest{
		ServiceId:         "svc-missing-readiness",
		ServiceName:       "missing-readiness",
		DisplayName:       "Missing Readiness",
		Host:              "missing-readiness.apps.example.com",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     80,
		InstanceClass:     "small",
		Exposure:          "public",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpsertService error = %v, want InvalidArgument", err)
	}
}
