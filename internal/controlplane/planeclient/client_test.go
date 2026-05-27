package planeclient

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubControlPlaneSouthbound struct {
	cloudplanev1.UnimplementedControlPlaneSnapshotServiceServer
	cloudplanev1.UnimplementedControlPlaneProjectServiceServer
	cloudplanev1.UnimplementedControlPlaneWorkloadServiceServer

	t                 *testing.T
	wantAuthorization string
}

func (s *stubControlPlaneSouthbound) GetSnapshot(ctx context.Context, _ *emptypb.Empty) (*cloudplanev1.PlaneSnapshot, error) {
	s.assertAuthorization(ctx)
	summary, err := structpb.NewStruct(map[string]any{
		"provider": map[string]any{
			"name": "aliyun",
		},
		"nodeAgent": map[string]any{
			"bootstrapTokenConfigured": true,
		},
	})
	if err != nil {
		s.t.Fatalf("build runtime config summary: %v", err)
	}
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
		Overview: &cloudplanev1.PlaneOverview{
			ProjectsTotal: 3,
			ServicesTotal: 7,
		},
		Capacity: &cloudplanev1.PlaneCapacity{
			RuntimeNodesTotal: 2,
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: 1,
		},
		RuntimeConfig: &cloudplanev1.PlaneRuntimeConfig{
			Fingerprint: "runtime-fp-a",
			Summary:     summary,
		},
	}, nil
}

func (s *stubControlPlaneSouthbound) ApplyProject(ctx context.Context, req *cloudplanev1.ApplyProjectRequest) (*cloudplanev1.ApplyProjectResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetProjectId() != "proj-global" {
		s.t.Fatalf("unexpected project id: %q", req.GetProjectId())
	}
	if req.GetOwnerUserId() != "usr-owner-001" {
		s.t.Fatalf("unexpected owner user id: %q", req.GetOwnerUserId())
	}
	return &cloudplanev1.ApplyProjectResponse{
		Action:    "created",
		ProjectId: req.GetProjectId(),
	}, nil
}

func (s *stubControlPlaneSouthbound) ApplyService(ctx context.Context, req *cloudplanev1.ApplyServiceRequest) (*cloudplanev1.ApplyServiceResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetProjectId() != "proj-global" {
		s.t.Fatalf("unexpected project id: %q", req.GetProjectId())
	}
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	if req.GetName() != "svc-demo" {
		s.t.Fatalf("unexpected service name: %q", req.GetName())
	}
	return &cloudplanev1.ApplyServiceResponse{
		Action:            "updated",
		DesiredGeneration: 12,
	}, nil
}

func (s *stubControlPlaneSouthbound) DeleteService(ctx context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	s.assertAuthorization(ctx)
	if req.GetProjectId() != "proj-global" {
		s.t.Fatalf("unexpected project id: %q", req.GetProjectId())
	}
	if req.GetServiceId() != "svc-1" {
		s.t.Fatalf("unexpected service id: %q", req.GetServiceId())
	}
	return &cloudplanev1.DeleteServiceResponse{
		Deleted:   true,
		ServiceId: req.GetServiceId(),
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
	cloudplanev1.RegisterControlPlaneProjectServiceServer(grpcServer, stub)
	cloudplanev1.RegisterControlPlaneWorkloadServiceServer(grpcServer, stub)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	defer server.Close()
	defer grpcServer.Stop()

	client, err := New(strings.TrimPrefix(server.URL, "http://"), "test-token")
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
	if snapshot.Plane.Name != "plane-a" || snapshot.Overview.ServicesTotal != 7 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.RuntimeConfig.Fingerprint != "runtime-fp-a" {
		t.Fatalf("unexpected runtime config fingerprint: %+v", snapshot.RuntimeConfig)
	}

	projectResp, err := client.ApplyProject(context.Background(), cloudplaneapi.Project{
		ID:          "proj-global",
		Name:        "demo",
		DisplayName: "Demo",
		OwnerUserID: "usr-owner-001",
	})
	if err != nil {
		t.Fatalf("ApplyProject returned error: %v", err)
	}
	if projectResp.Action != cloudplaneapi.ApplyActionCreated || projectResp.ProjectID != "proj-global" {
		t.Fatalf("unexpected apply project response: %+v", projectResp)
	}

	applyResp, err := client.ApplyService(context.Background(), "proj-global", "svc-1", "svc-demo", cloudplaneapi.ApplyServiceRequest{
		DisplayName: "Demo Service",
		Spec: cloudplaneapi.ServiceSpec{
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: "u1",
			Image:         "nginx:latest",
			DefaultPort:   8080,
			ProjectedFiles: []projectedfile.Spec{
				{MountPath: "/etc/app/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
			},
			PersistentDirs: []persistentdir.Spec{
				{Name: "data", MountPath: "/var/lib/app"},
			},
		},
	})
	if err != nil {
		t.Fatalf("ApplyService returned error: %v", err)
	}
	if applyResp.Action != cloudplaneapi.ApplyActionUpdated || applyResp.DesiredGeneration != 12 {
		t.Fatalf("unexpected service response: %+v", applyResp)
	}

	if err := client.DeleteService(context.Background(), "proj-global", "svc-1"); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}
}

func TestClientMapsNotFound(t *testing.T) {
	grpcServer := grpc.NewServer()
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, notFoundControlPlaneSouthbound{})

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	defer server.Close()
	defer grpcServer.Stop()

	client, err := New(strings.TrimPrefix(server.URL, "http://"), "test-token")
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
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
