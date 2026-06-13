package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

func TestBuildServiceResourceIncludesEnv(t *testing.T) {
	t.Parallel()

	resource := buildServiceResource(model.Service{
		Metadata: model.ServiceMetadata{
			ID:          "svc_test",
			Name:        "demo-api",
			DisplayName: "Demo API",
			Host:        "demo-api.apps.example.test",
			Generation:  1,
		},
		Spec: model.ServiceSpec{
			PlaneID:       "pln_test",
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "registry.example.com/demo/api:v1",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
			Env: map[string]string{
				"MODE":      "demo",
				"API_TOKEN": "service-token",
			},
		},
		Status: model.ServiceStatus{
			DesiredState: model.DesiredStateActive,
			Observed: model.ServiceObservedStatus{
				Phase: model.PhasePending,
			},
			Run: model.RunStatus{Phase: model.RunPhasePending},
		},
	})

	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal service resource returned error: %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, "API_TOKEN") || !strings.Contains(body, "service-token") {
		t.Fatalf("service resource did not include env: %s", body)
	}
	if !strings.Contains(body, `"generation":1`) || !strings.Contains(body, `"observedGeneration":0`) {
		t.Fatalf("service resource did not include generation state: %s", body)
	}
}

func TestBuildServiceResourceKeepsEmptyWorkloadFields(t *testing.T) {
	t.Parallel()

	resource := buildServiceResource(model.Service{
		Metadata: model.ServiceMetadata{
			ID:          "svc_test",
			Name:        "demo-api",
			DisplayName: "Demo API",
			Host:        "demo-api.apps.example.test",
			Generation:  1,
		},
		Spec: model.ServiceSpec{
			PlaneID:       "pln_test",
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
		Status: model.ServiceStatus{
			DesiredState: model.DesiredStateActive,
			Observed: model.ServiceObservedStatus{
				Phase: model.PhasePending,
			},
			Run: model.RunStatus{Phase: model.RunPhasePending},
		},
	})

	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal service resource returned error: %v", err)
	}
	body := string(payload)
	for _, want := range []string{`"command":[]`, `"args":[]`, `"env":{}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("service resource did not include %s: %s", want, body)
		}
	}
}

func TestUpdateServiceRequiresPlaneIDQuery(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceAPIPlane(t)
	plane, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "api-plane",
		DisplayName:  "API Plane",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: planeServer.endpoint,
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, plane.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusReady, Message: "ready"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	planeSyncer := coordination.NewPlaneSyncer(logger, db.Store, "southbound-token", nil)
	services := coordination.NewServiceOperations(logger, db.Store, "southbound-token", "apps.example.test", planeSyncer)
	handler, err := NewMux(Options{
		AdminToken:        "admin-token",
		SouthboundToken:   "southbound-token",
		ServiceOperations: services,
		PlaneSyncer:       planeSyncer,
	}, logger, db.Store)
	if err != nil {
		t.Fatalf("NewMux returned error: %v", err)
	}

	createBody := []byte(`{
		"name": "strict-web",
		"displayName": "Strict Web",
		"spec": {
			"planeID": "` + plane.ID + `",
			"instanceClass": "small",
			"exposure": "public",
			"image": "nginx:1.27-alpine",
			"defaultPort": 80,
			"readinessPath": "/"
		}
	}`)
	createResp := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/services", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer admin-token")
	createReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createResp.Code, createResp.Body.String())
	}
	var created serviceResource
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	updateBody := []byte(`{
		"displayName": "Strict Web v2",
		"spec": {
			"instanceClass": "small",
			"exposure": "public",
			"image": "nginx:1.28-alpine",
			"defaultPort": 80,
			"readinessPath": "/"
		}
	}`)
	updateResp := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/services/"+created.Metadata.ID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer admin-token")
	updateReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d body = %s, want 400 without planeID query", updateResp.Code, updateResp.Body.String())
	}

	updateResp = httptest.NewRecorder()
	updateReq = httptest.NewRequest(http.MethodPut, "/api/v1/services/"+created.Metadata.ID+"?planeID="+plane.ID, bytes.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer admin-token")
	updateReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s, want 200 with planeID query", updateResp.Code, updateResp.Body.String())
	}
}

type serviceAPIPlane struct {
	cloudplanev1.UnimplementedCloudPlaneServiceServer
	cloudplanev1.UnimplementedCloudPlaneSnapshotServiceServer

	mu       sync.Mutex
	endpoint string
	service  *cloudplanev1.PlaneService
}

func startServiceAPIPlane(t *testing.T) *serviceAPIPlane {
	t.Helper()

	grpcServer := grpc.NewServer()
	plane := &serviceAPIPlane{}
	cloudplanev1.RegisterCloudPlaneServiceServer(grpcServer, plane)
	cloudplanev1.RegisterCloudPlaneSnapshotServiceServer(grpcServer, plane)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	t.Cleanup(func() {
		server.Close()
		grpcServer.Stop()
	})
	plane.endpoint = strings.TrimPrefix(server.URL, "http://")
	return plane
}

func (p *serviceAPIPlane) GetSnapshot(context.Context, *cloudplanev1.GetSnapshotRequest) (*cloudplanev1.GetSnapshotResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	services := make([]*cloudplanev1.PlaneService, 0, 1)
	if p.service != nil {
		services = append(services, p.service)
	}
	return &cloudplanev1.GetSnapshotResponse{Snapshot: &cloudplanev1.PlaneSnapshot{
		Plane:         &cloudplanev1.PlaneSummary{Name: "api-plane", Provider: "aliyun", Region: "cn-beijing"},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{},
		Services:      services,
	}}, nil
}

func (p *serviceAPIPlane) UpsertService(_ context.Context, req *cloudplanev1.UpsertServiceRequest) (*cloudplanev1.UpsertServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.service = &cloudplanev1.PlaneService{
		ServiceId:     req.GetServiceId(),
		Name:          req.GetServiceName(),
		DisplayName:   req.GetDisplayName(),
		Host:          req.GetHost(),
		Generation:    req.GetServiceGeneration(),
		DesiredState:  model.DesiredStateActive,
		InstanceClass: req.GetInstanceClass(),
		Exposure:      req.GetExposure(),
		Image:         req.GetImage(),
		ContainerPort: req.GetContainerPort(),
		ReadinessPath: req.GetReadinessPath(),
	}
	return &cloudplanev1.UpsertServiceResponse{}, nil
}

func (p *serviceAPIPlane) DeleteService(context.Context, *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	return &cloudplanev1.DeleteServiceResponse{}, nil
}
