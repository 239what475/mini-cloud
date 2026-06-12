package coordination

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type stubControlPlaneSouthbound struct {
	cloudplanev1.UnimplementedCloudPlaneSnapshotServiceServer
	cloudplanev1.UnimplementedCloudPlaneServiceServer

	t                 *testing.T
	wantAuthorization string
}

func (s *stubControlPlaneSouthbound) GetSnapshot(ctx context.Context, _ *cloudplanev1.GetSnapshotRequest) (*cloudplanev1.GetSnapshotResponse, error) {
	s.assertAuthorization(ctx)
	return &cloudplanev1.GetSnapshotResponse{Snapshot: &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:     "plane-a",
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: 1,
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{
				ServiceId:         "svc-1",
				ServiceName:       "svc-demo",
				ServiceGeneration: 12,
				Status:            "running",
			},
		},
	}}, nil
}

func (s *stubControlPlaneSouthbound) UpsertService(ctx context.Context, req *cloudplanev1.UpsertServiceRequest) (*cloudplanev1.UpsertServiceResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	if req.GetServiceName() != "svc-demo" {
		s.t.Fatalf("unexpected service name: %q", req.GetServiceName())
	}
	return &cloudplanev1.UpsertServiceResponse{}, nil
}

func (s *stubControlPlaneSouthbound) DeleteService(ctx context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	if req.GetServiceGeneration() != 13 {
		s.t.Fatalf("unexpected service generation: %d", req.GetServiceGeneration())
	}
	return &cloudplanev1.DeleteServiceResponse{}, nil
}

func (s *stubControlPlaneSouthbound) assertAuthorization(ctx context.Context) {
	s.t.Helper()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		s.t.Fatal("missing incoming metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 || values[0] != s.wantAuthorization {
		s.t.Fatalf("unexpected authorization header: %v", values)
	}
}

type notFoundControlPlaneSouthbound struct {
	cloudplanev1.UnimplementedCloudPlaneSnapshotServiceServer
}

func (notFoundControlPlaneSouthbound) GetSnapshot(context.Context, *cloudplanev1.GetSnapshotRequest) (*cloudplanev1.GetSnapshotResponse, error) {
	return nil, status.Error(codes.NotFound, "plane not found")
}

func TestClientUsesGRPCSouthbound(t *testing.T) {
	grpcServer := grpc.NewServer()
	stub := &stubControlPlaneSouthbound{
		t:                 t,
		wantAuthorization: "Bearer test-token",
	}
	cloudplanev1.RegisterCloudPlaneSnapshotServiceServer(grpcServer, stub)
	cloudplanev1.RegisterCloudPlaneServiceServer(grpcServer, stub)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	defer server.Close()
	defer grpcServer.Stop()

	client, err := newPlaneClient(strings.TrimPrefix(server.URL, "http://"), "test-token")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close client: %v", err)
		}
	}()

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if snapshot.GetPlane().GetName() != "plane-a" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.GetExecutions()) != 1 || snapshot.GetExecutions()[0].GetServiceId() != "svc-1" || snapshot.GetExecutions()[0].GetStatus() != "running" {
		t.Fatalf("unexpected execution snapshots: %+v", snapshot.GetExecutions())
	}

	if err := client.UpsertService(context.Background(), &cloudplanev1.UpsertServiceRequest{
		ServiceId:         "svc-1",
		ServiceName:       "svc-demo",
		DisplayName:       "Demo",
		ServiceGeneration: 12,
		Image:             "nginx:latest",
		ContainerPort:     8080,
		ReadinessPath:     "/healthz",
		InstanceClass:     "small",
	}); err != nil {
		t.Fatalf("UpsertService returned error: %v", err)
	}

	if err := client.DeleteService(context.Background(), &cloudplanev1.DeleteServiceRequest{
		ServiceId:         "svc-1",
		ServiceGeneration: 13,
	}); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}
}

func TestClientMapsNotFound(t *testing.T) {
	grpcServer := grpc.NewServer()
	cloudplanev1.RegisterCloudPlaneSnapshotServiceServer(grpcServer, notFoundControlPlaneSouthbound{})

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	defer server.Close()
	defer grpcServer.Stop()

	client, err := newPlaneClient(strings.TrimPrefix(server.URL, "http://"), "test-token")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close client: %v", err)
		}
	}()

	_, err = client.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errPlaneObjectNotFound) {
		t.Fatalf("expected errPlaneObjectNotFound, got %v", err)
	}
}
