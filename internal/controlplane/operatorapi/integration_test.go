package operatorapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/operatorapi"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planeselector"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/servicecontroller"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"
	"mini-cloud/internal/operatorclient"
	"mini-cloud/internal/testutil"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestOperatorTransportServesGRPCAndGateway(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	planeItem, err := db.Store.CreatePlane(context.Background(), plane.CreateInput{
		Name:         "aliyun-bj-primary",
		DisplayName:  "Aliyun Beijing Primary",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}

	_, err = db.Store.CreateService(context.Background(), controlservice.CreateInput{
		Name:        "hello",
		DisplayName: "Hello",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "nginx:1.27-alpine",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	controller := servicecontroller.New(logger, db.Store, &fakePlanner{
		previewResult: planeselector.SelectionResult{
			Decision: &planeselector.Decision{
				PlaneID:                     planeItem.ID,
				PlaneName:                   "aliyun-bj-primary",
				PlaneDisplayName:            "Aliyun Beijing Primary",
				Provider:                    "aliyun",
				Region:                      "cn-beijing",
				BasedOnInventorySyncVersion: 1,
			},
		},
	}, &fakeDeploy{})
	transports, err := operatorapi.NewTransportSet(
		logger,
		db.Store,
		"operator-admin",
		nil,
		controller,
	)
	if err != nil {
		t.Fatalf("NewTransportSet returned error: %v", err)
	}

	publicMux := http.NewServeMux()
	publicMux.Handle("/api/operator/v1/", transports.GatewayHTTP)
	publicMux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if operatorapi.IsGRPCRequest(r) {
			transports.GRPC.ServeHTTP(w, r)
			return
		}
		publicMux.ServeHTTP(w, r)
	}), &http2.Server{}))
	defer server.Close()

	client, err := operatorclient.New(server.URL, "operator-admin")
	if err != nil {
		t.Fatalf("operatorclient.New returned error: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close client: %v", err)
		}
	}()

	overview, err := client.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview returned error: %v", err)
	}
	if overview.GetServicesTotal() != 1 || overview.GetPlanesTotal() != 1 {
		t.Fatalf("unexpected overview payload: %+v", overview)
	}

	services, err := client.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices returned error: %v", err)
	}
	if len(services) != 1 || services[0].GetMetadata().GetName() != "hello" {
		t.Fatalf("unexpected services payload: %+v", services)
	}

	created, err := client.CreateService(context.Background(), &controlplanev1.CreateServiceRequest{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Spec: &controlplanev1.ServiceSpec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
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
	if created.GetMetadata().GetName() != "cliproxyapi" {
		t.Fatalf("created service name = %q, want cliproxyapi", created.GetMetadata().GetName())
	}
	if created.GetStatus().GetPlacement() == nil || created.GetStatus().GetPlacement().GetPlaneId() == "" {
		t.Fatalf("expected created service placement, got %+v", created.GetStatus().GetPlacement())
	}

	resp, err := http.NewRequest(http.MethodGet, server.URL+"/api/operator/v1/overview", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	resp.Header.Set("Authorization", "Bearer operator-admin")
	httpResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatalf("gateway request returned error: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Logf("close http response body: %v", err)
		}
	}()
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d, want 200", httpResp.StatusCode)
	}

}

type fakePlanner struct {
	previewResult planeselector.SelectionResult
}

func (f *fakePlanner) PreviewSelection(_ context.Context, _ planeselector.SelectionInput) (planeselector.SelectionResult, error) {
	return f.previewResult, nil
}

type fakeDeploy struct{}

func (f *fakeDeploy) ApplyService(_ context.Context, planeID string, input deploy.ApplyServiceInput) (deploy.ApplyResult, error) {
	return deploy.ApplyResult{
		PlaneID: planeID,
		Action:  deploy.ApplyActionCreated,
		PlanID:  input.Metadata.ID + "-g1",
	}, nil
}

func (f *fakeDeploy) DeleteService(context.Context, string, deploy.DeleteServiceInput) error {
	return nil
}
