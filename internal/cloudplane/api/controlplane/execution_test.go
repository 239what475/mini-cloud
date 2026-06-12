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
	server := newExecutionServer(slog.Default(), db.Store, newAuthenticator("southbound-token"))

	_, err := server.DeleteService(ctx, &cloudplanev1.DeleteServiceRequest{
		ServiceId:         "missing-service",
		ServiceGeneration: 1,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("DeleteService error = %v, want NotFound", err)
	}
}
