package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCloudPlaneConfig(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, validCloudPlaneYAML())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.Plane.Name != "mini-cloud-ops" || cfg.Infrastructure.Provider != "aliyun" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, validCloudPlaneYAML()+"\nunknownNodeField: true\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want unknown field rejection")
	}
}

func TestValidateCloudPlaneConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "unsupported provider",
			mutate: func(cfg *Config) {
				cfg.Infrastructure.Provider = "local"
			},
		},
		{
			name: "missing aliyun node config",
			mutate: func(cfg *Config) {
				cfg.NodeProvisioning.Aliyun = AliyunNodeConfig{}
			},
		},
		{
			name: "missing node provisioning instance type",
			mutate: func(cfg *Config) {
				cfg.NodeProvisioning.InstanceType = ""
			},
		},
		{
			name: "missing caddy admin url when ingress enabled",
			mutate: func(cfg *Config) {
				cfg.Ingress = IngressConfig{BaseDomain: "apps.example.test"}
			},
		},
		{
			name: "public origin without base domain",
			mutate: func(cfg *Config) {
				cfg.Ingress = IngressConfig{
					CaddyAdminURL: "http://127.0.0.1:2019",
					PublicOrigin:  "203.0.113.10",
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate error = nil, want validation error")
			}
		})
	}
}

func TestValidateConnectEndpointAcceptsGRPCTargets(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"10.0.0.10:18081",
		"grpc://10.0.0.10:18081",
		"grpcs://10.0.0.10:18081",
	} {
		endpoint := endpoint
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.NodeAgent.ConnectEndpoint = endpoint
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate error = %v", err)
			}
		})
	}
}

func validConfig() Config {
	return Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		TLS:          TLSConfig{CACert: "ca", Cert: "cert", Key: "key"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-ops", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{BearerToken: "southbound-token"},
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

func validCloudPlaneYAML() string {
	return `
server:
  listenGRPCAddr: 0.0.0.0:18081
tls:
  caCert: ca
  cert: cert
  key: key
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  name: mini-cloud-ops
  grpcEndpoint: 10.0.0.10:18081
controlPlane:
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
`
}

func writeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cloud-plane.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	return path
}
