package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAcceptsMinimalCloudPlaneConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  name: mini-cloud-lab
  grpcEndpoint: 10.0.0.10:18081
controlPlane:
  url: http://127.0.0.1:18080
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  token: node-agent-token
  binaryUrl: https://artifact.example/node-agent-linux-amd64
infrastructure:
  provider: aliyun
  regionId: cn-beijing
nodeProvisioning:
  instanceType: ecs.u1-c1m1.large
  aliyun:
    imageId: m-test
    vSwitchId: vsw-test
    securityGroupId: sg-test
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.Plane.Name != "mini-cloud-lab" {
		t.Fatalf("Plane.Name = %q, want mini-cloud-lab", cfg.Plane.Name)
	}
	if cfg.Infrastructure.Provider != "aliyun" {
		t.Fatalf("Infrastructure.Provider = %q, want aliyun", cfg.Infrastructure.Provider)
	}
}

func TestValidateAcceptsHTTPNodeAgentConnectEndpoint(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "https://10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		NodeProvisioning: NodeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			Aliyun: AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

func TestValidateRejectsLocalProvider(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "local", RegionID: "local"},
		NodeProvisioning: NodeProvisioningConfig{
			InstanceType: "local",
			Aliyun: AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want local provider rejection")
	}
}

func TestValidateRequiresAliyunNodeConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure:   InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		NodeProvisioning: NodeProvisioningConfig{InstanceType: "ecs.u1-c1m1.large"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want aliyun node provisioning requirement")
	}
}

func TestValidateRequiresNodeProvisioningInstanceType(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		NodeProvisioning: NodeProvisioningConfig{
			Aliyun: AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want node provisioning instanceType requirement")
	}
}

func TestValidateRequiresCaddyAdminURLWhenIngressEnabled(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		NodeProvisioning: NodeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			Aliyun: AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
		Ingress: IngressConfig{BaseDomain: "apps.example.test"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want caddy admin URL requirement")
	}
}

func TestValidateAcceptsCaddyAdminURLWithoutIngressDomain(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Ingress = IngressConfig{CaddyAdminURL: "http://127.0.0.1:2019"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

func TestValidateRequiresBaseDomainWhenFrontDoorConfigured(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Ingress = IngressConfig{
		CaddyAdminURL: "http://127.0.0.1:2019",
		PublicOrigin:  "203.0.113.10",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want baseDomain requirement")
	}

	cfg.Ingress.BaseDomain = "apps.example.com"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

func validConfig() Config {
	return Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		NodeProvisioning: NodeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			Aliyun: AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  name: mini-cloud-lab
controlPlane:
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  token: node-agent-token
infrastructure:
  provider: aliyun
  regionId: cn-beijing
unknownNodeField: true
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want unknown field rejection")
	}
}
