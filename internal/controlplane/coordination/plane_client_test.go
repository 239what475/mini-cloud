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
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubControlPlaneSouthbound struct {
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer
	cloudplanev1.UnimplementedControlPlaneExecutionServiceServer

	t                 *testing.T
	wantAuthorization string
}

func (s *stubControlPlaneSouthbound) GetSnapshot(ctx context.Context, _ *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	s.assertAuthorization(ctx)
	return &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:       "plane-a",
			Provider:   "aliyun",
			Region:     "cn-beijing",
			Configured: true,
		},
		Health: &cloudplanev1.PlaneHealth{
			Service:  "ok",
			Database: "ok",
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: 1,
		},
		RuntimeInventory: &cloudplanev1.PlaneRuntimeInventory{},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{
				PlanId:            "svc-1-g12",
				ServiceId:         "svc-1",
				ServiceName:       "svc-demo",
				ServiceGeneration: 12,
				Status:            "running",
			},
		},
	}, nil
}

func (s *stubControlPlaneSouthbound) ApplyExecutionPlan(ctx context.Context, req *cloudplanev1.ApplyExecutionPlanRequest) (*cloudplanev1.ApplyExecutionPlanResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	if req.GetServiceName() != "svc-demo" {
		s.t.Fatalf("unexpected service name: %q", req.GetServiceName())
	}
	if req.GetPlanId() != "svc-1-g12" {
		s.t.Fatalf("unexpected plan id: %q", req.GetPlanId())
	}
	return &cloudplanev1.ApplyExecutionPlanResponse{
		Action: "accepted",
		PlanId: req.GetPlanId(),
	}, nil
}

func (s *stubControlPlaneSouthbound) DeleteExecutionPlan(ctx context.Context, req *cloudplanev1.DeleteExecutionPlanRequest) (*cloudplanev1.DeleteExecutionPlanResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	if req.GetServiceGeneration() != 13 {
		s.t.Fatalf("unexpected service generation: %d", req.GetServiceGeneration())
	}
	if req.GetPlanId() != "svc-1-delete-g13" {
		s.t.Fatalf("unexpected delete plan id: %q", req.GetPlanId())
	}
	return &cloudplanev1.DeleteExecutionPlanResponse{
		ServiceId: req.GetServiceId(),
		Deleted:   true,
	}, nil
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
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer
}

func (notFoundControlPlaneSouthbound) GetSnapshot(context.Context, *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	return nil, status.Error(codes.NotFound, "plane not found")
}

func TestClientUsesGRPCSouthbound(t *testing.T) {
	grpcServer := grpc.NewServer()
	stub := &stubControlPlaneSouthbound{
		t:                 t,
		wantAuthorization: "Bearer test-token",
	}
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, stub)
	cloudplanev1.RegisterControlPlaneExecutionServiceServer(grpcServer, stub)

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
	if len(snapshot.GetExecutions()) != 1 || snapshot.GetExecutions()[0].GetPlanId() != "svc-1-g12" || snapshot.GetExecutions()[0].GetStatus() != "running" {
		t.Fatalf("unexpected execution snapshots: %+v", snapshot.GetExecutions())
	}

	applyResp, err := client.ApplyExecutionPlan(context.Background(), &cloudplanev1.ApplyExecutionPlanRequest{
		PlanId:            "svc-1-g12",
		ServiceId:         "svc-1",
		ServiceName:       "svc-demo",
		ServiceGeneration: 12,
		Image:             "nginx:latest",
		ContainerPort:     8080,
		ReadinessPath:     "/healthz",
		InstanceClass:     "small",
		ProjectedFiles: []*cloudplanev1.ExecutionProjectedFile{
			{MountPath: "/etc/app/config.yaml", Content: "app: demo", Mode: 0444},
		},
	})
	if err != nil {
		t.Fatalf("ApplyExecutionPlan returned error: %v", err)
	}
	if applyResp.GetAction() != "accepted" || applyResp.GetPlanId() != "svc-1-g12" {
		t.Fatalf("unexpected execution plan response: %+v", applyResp)
	}

	if err := client.DeleteExecutionPlan(context.Background(), &cloudplanev1.DeleteExecutionPlanRequest{
		ServiceId:         "svc-1",
		ServiceGeneration: 13,
		PlanId:            "svc-1-delete-g13",
	}); err != nil {
		t.Fatalf("DeleteExecutionPlan returned error: %v", err)
	}
}

func TestClientMapsNotFound(t *testing.T) {
	grpcServer := grpc.NewServer()
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, notFoundControlPlaneSouthbound{})

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
