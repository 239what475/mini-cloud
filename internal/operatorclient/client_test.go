package operatorclient

import (
	"context"
	"net/http/httptest"
	"testing"

	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestClientAddsBearerToken(t *testing.T) {
	t.Parallel()

	var seenAuth string
	client := newBufconnClient(t, &testOperatorService{
		getOverview: func(ctx context.Context, _ *emptypb.Empty) (*controlplanev1.Overview, error) {
			seenAuth = firstMetadataValue(ctx, "authorization")
			return &controlplanev1.Overview{PlanesTotal: 2}, nil
		},
	}, "operator-secret")

	overview, err := client.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview returned error: %v", err)
	}
	if overview.GetPlanesTotal() != 2 {
		t.Fatalf("planes total = %d, want 2", overview.GetPlanesTotal())
	}
	if seenAuth != "Bearer operator-secret" {
		t.Fatalf("authorization = %q, want bearer token", seenAuth)
	}
}

func TestClientListsServices(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testOperatorService{
		listServices: func(ctx context.Context, req *emptypb.Empty) (*controlplanev1.ListServicesResponse, error) {
			return &controlplanev1.ListServicesResponse{
				Items: []*controlplanev1.Service{{Metadata: &controlplanev1.ServiceMetadata{Id: "svc_demo", Name: "hello"}}},
			}, nil
		},
	}, "")

	services, err := client.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices returned error: %v", err)
	}
	if len(services) != 1 || services[0].GetMetadata().GetId() != "svc_demo" {
		t.Fatalf("unexpected services payload: %+v", services)
	}
}

func TestClientCreatesService(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testOperatorService{
		createService: func(ctx context.Context, req *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error) {
			if req.GetName() != "cliproxyapi" {
				t.Fatalf("name = %q, want cliproxyapi", req.GetName())
			}
			if req.GetSpec().GetImage() != "ghcr.io/example/cliproxyapi:demo" {
				t.Fatalf("image = %q", req.GetSpec().GetImage())
			}
			return &controlplanev1.Service{Metadata: &controlplanev1.ServiceMetadata{Id: "svc_demo", Name: req.GetName()}}, nil
		},
	}, "")

	service, err := client.CreateService(context.Background(), &controlplanev1.CreateServiceRequest{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Spec: &controlplanev1.ServiceSpec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			InstanceClass: "small",
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:demo",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if service.GetMetadata().GetId() != "svc_demo" {
		t.Fatalf("service id = %q, want svc_demo", service.GetMetadata().GetId())
	}
}

type testOperatorService struct {
	controlplanev1.UnimplementedOperatorServiceServer

	getOverview   func(context.Context, *emptypb.Empty) (*controlplanev1.Overview, error)
	listServices  func(context.Context, *emptypb.Empty) (*controlplanev1.ListServicesResponse, error)
	createService func(context.Context, *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error)
}

func (s *testOperatorService) GetOverview(ctx context.Context, req *emptypb.Empty) (*controlplanev1.Overview, error) {
	if s.getOverview != nil {
		return s.getOverview(ctx, req)
	}
	return &controlplanev1.Overview{}, nil
}

func (s *testOperatorService) ListServices(ctx context.Context, req *emptypb.Empty) (*controlplanev1.ListServicesResponse, error) {
	if s.listServices != nil {
		return s.listServices(ctx, req)
	}
	return &controlplanev1.ListServicesResponse{}, nil
}

func (s *testOperatorService) CreateService(ctx context.Context, req *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error) {
	if s.createService != nil {
		return s.createService(ctx, req)
	}
	return &controlplanev1.Service{}, nil
}

func newBufconnClient(t *testing.T, server controlplanev1.OperatorServiceServer, token string) *Client {
	t.Helper()

	grpcServer := grpc.NewServer()
	controlplanev1.RegisterOperatorServiceServer(grpcServer, server)
	testServer := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	t.Cleanup(func() {
		testServer.Close()
		grpcServer.Stop()
	})

	client, err := New(testServer.URL, token)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("close operator client: %v", err)
		}
	})
	return client
}

func firstMetadataValue(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
