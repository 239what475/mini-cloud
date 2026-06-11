package cloudplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

func TestRegisterWithControlPlane(t *testing.T) {
	var got registrationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/planes/register" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer southbound-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := registerWithControlPlane(context.Background(), cloudplaneconfig.Config{
		Plane: cloudplaneconfig.PlaneConfig{
			Name:         "mini-cloud-lab",
			GRPCEndpoint: "10.0.0.10:18081",
		},
		ControlPlane: cloudplaneconfig.ControlPlaneConfig{
			URL:         server.URL,
			BearerToken: "southbound-token",
		},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: "tencent",
			RegionID: "ap-guangzhou",
		},
	})
	if err != nil {
		t.Fatalf("registerWithControlPlane returned error: %v", err)
	}
	if got.Name != "mini-cloud-lab" || got.GRPCEndpoint != "10.0.0.10:18081" || got.Provider != "tencent" || got.Region != "ap-guangzhou" {
		t.Fatalf("unexpected registration body: %+v", got)
	}
}
