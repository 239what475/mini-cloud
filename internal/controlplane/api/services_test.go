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

	controlplaneconfig "mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/coordination"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

func TestUpdateServiceRequiresPlaneIDQuery(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceAPIPlane(t)
	catalog, err := coordination.NewPlaneCatalog([]controlplaneconfig.PlaneConfig{{
		ID:           "pln_api",
		Name:         "api-plane",
		DisplayName:  "API Plane",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: planeServer.endpoint,
	}})
	if err != nil {
		t.Fatalf("NewPlaneCatalog returned error: %v", err)
	}
	plane, err := catalog.GetPlane(ctx, "pln_api")
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	planeSyncer := coordination.NewPlaneSyncer(logger, catalog, "southbound-token", coordination.PlaneClientTLS{}, nil)
	services := coordination.NewServiceOperations(logger, catalog, "southbound-token", coordination.PlaneClientTLS{}, "apps.example.test", planeSyncer)
	handler, err := NewMux(Options{
		AdminToken:        "admin-token",
		SouthboundToken:   "southbound-token",
		ServiceOperations: services,
		PlaneSyncer:       planeSyncer,
	}, logger)
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
		DesiredState:  "active",
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
