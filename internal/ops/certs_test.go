package ops

import (
	"path/filepath"
	"testing"
)

func TestLoadCertificateBundleReusesLocalState(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	runner := NewRunner(Config{Path: cfgPath})
	plans := []cloudPlaneInstallPlan{
		{
			Plane:     Plane{Name: "tencent"},
			PrivateIP: "10.0.0.10",
		},
	}

	first, err := runner.loadCertificateBundle(plans)
	if err != nil {
		t.Fatalf("loadCertificateBundle first call returned error: %v", err)
	}
	second, err := runner.loadCertificateBundle(plans)
	if err != nil {
		t.Fatalf("loadCertificateBundle second call returned error: %v", err)
	}

	if first.CA.Cert == "" || first.ControlPlane.Cert == "" || first.CloudPlanes["tencent"].Cert == "" {
		t.Fatalf("certificate bundle is incomplete: %+v", first)
	}
	if first.CA.Cert != second.CA.Cert {
		t.Fatal("CA certificate was regenerated")
	}
	if first.ControlPlane.Cert != second.ControlPlane.Cert {
		t.Fatal("control-plane certificate was regenerated")
	}
	if first.CloudPlanes["tencent"].Cert != second.CloudPlanes["tencent"].Cert {
		t.Fatal("cloud-plane certificate was regenerated")
	}
}

func TestLoadCertificateBundleReissuesCloudPlaneCertWhenEndpointChanges(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	runner := NewRunner(Config{Path: cfgPath})

	first, err := runner.loadCertificateBundle([]cloudPlaneInstallPlan{
		{
			Plane:     Plane{Name: "tencent"},
			PrivateIP: "10.0.0.10",
		},
	})
	if err != nil {
		t.Fatalf("loadCertificateBundle first call returned error: %v", err)
	}
	second, err := runner.loadCertificateBundle([]cloudPlaneInstallPlan{
		{
			Plane:     Plane{Name: "tencent"},
			PrivateIP: "10.0.0.11",
		},
	})
	if err != nil {
		t.Fatalf("loadCertificateBundle second call returned error: %v", err)
	}

	if first.CA.Cert != second.CA.Cert {
		t.Fatal("CA certificate was regenerated")
	}
	if first.ControlPlane.Cert != second.ControlPlane.Cert {
		t.Fatal("control-plane certificate was regenerated")
	}
	if first.CloudPlanes["tencent"].Cert == second.CloudPlanes["tencent"].Cert {
		t.Fatal("cloud-plane certificate was not regenerated after endpoint changed")
	}
}
