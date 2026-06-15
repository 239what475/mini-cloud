package ops

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type certificateBundle struct {
	CA           tlsTemplateData
	ControlPlane tlsTemplateData
	CloudPlanes  map[string]tlsTemplateData
}

type certificateState struct {
	CA           tlsTemplateData            `json:"ca"`
	ControlPlane tlsTemplateData            `json:"controlPlane"`
	CloudPlanes  map[string]tlsTemplateData `json:"cloudPlanes"`
}

func (r *Runner) loadCertificateBundle(plans []cloudPlaneInstallPlan) (certificateBundle, error) {
	path := r.certificateStatePath()
	state, err := readCertificateState(path)
	if err != nil {
		return certificateBundle{}, err
	}
	changed := false
	if state.CA.Cert == "" || state.CA.Key == "" {
		caCert, caKey, err := newCertificateAuthority()
		if err != nil {
			return certificateBundle{}, err
		}
		state.CA = tlsTemplateData{
			CACert: string(pemEncodeCertificate(caCert.Raw)),
			Cert:   string(pemEncodeCertificate(caCert.Raw)),
			Key:    string(pemEncodePrivateKey(caKey)),
		}
		changed = true
	}
	if state.CA.CACert == "" {
		state.CA.CACert = state.CA.Cert
		changed = true
	}
	caCert, caKey, err := parseCertificateAuthority(state.CA)
	if err != nil {
		return certificateBundle{}, err
	}
	if state.ControlPlane.Cert == "" || state.ControlPlane.Key == "" || state.ControlPlane.CACert != state.CA.Cert {
		cert, err := issueControlPlaneCertificate(caCert, caKey, state.CA.Cert)
		if err != nil {
			return certificateBundle{}, err
		}
		state.ControlPlane = cert
		changed = true
	}
	if state.CloudPlanes == nil {
		state.CloudPlanes = map[string]tlsTemplateData{}
	}
	for _, plan := range plans {
		current := state.CloudPlanes[plan.Plane.Name]
		if !cloudPlaneCertificateNeedsIssue(current, state.CA.Cert, plan) {
			continue
		}
		cert, err := issueCloudPlaneCertificate(caCert, caKey, state.CA.Cert, plan)
		if err != nil {
			return certificateBundle{}, err
		}
		state.CloudPlanes[plan.Plane.Name] = cert
		changed = true
	}
	if changed {
		if err := writeCertificateState(path, state); err != nil {
			return certificateBundle{}, err
		}
	}
	return certificateBundle{
		CA:           state.CA,
		ControlPlane: state.ControlPlane,
		CloudPlanes:  state.CloudPlanes,
	}, nil
}

func (r *Runner) certificateStatePath() string {
	dir := filepath.Dir(r.cfg.Path)
	if dir == "." || dir == "" {
		dir = "deploy/ops"
	}
	return filepath.Join(dir, "state", "tls.json")
}

func readCertificateState(path string) (certificateState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return certificateState{}, nil
		}
		return certificateState{}, fmt.Errorf("read TLS state %q: %w", path, err)
	}
	var state certificateState
	if err := json.Unmarshal(data, &state); err != nil {
		return certificateState{}, fmt.Errorf("parse TLS state %q: %w", path, err)
	}
	return state, nil
}

func writeCertificateState(path string, state certificateState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create TLS state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode TLS state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write TLS state %q: %w", path, err)
	}
	return nil
}

func parseCertificateAuthority(material tlsTemplateData) (*x509.Certificate, *rsa.PrivateKey, error) {
	certBlock, _ := pem.Decode([]byte(material.Cert))
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("TLS state CA certificate is invalid")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse TLS state CA certificate: %w", err)
	}
	keyBlock, _ := pem.Decode([]byte(material.Key))
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		return nil, nil, fmt.Errorf("TLS state CA private key is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse TLS state CA private key: %w", err)
	}
	return cert, key, nil
}

func newCertificateAuthority() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ca key: %w", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: "mini-cloud private ca"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create ca certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse ca certificate: %w", err)
	}
	return cert, key, nil
}

func newLeafCertificate(caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string, dnsNames []string, ipAddresses []net.IP, client bool) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate key: %w", err)
	}
	now := time.Now().UTC()
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if client {
		extKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  extKeyUsage,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate %s: %w", commonName, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate %s: %w", commonName, err)
	}
	return cert, key, nil
}

func issueControlPlaneCertificate(caCert *x509.Certificate, caKey *rsa.PrivateKey, caPEM string) (tlsTemplateData, error) {
	cert, key, err := newLeafCertificate(caCert, caKey, "mini-cloud-control-plane", nil, nil, true)
	if err != nil {
		return tlsTemplateData{}, err
	}
	return tlsTemplateData{
		CACert: caPEM,
		Cert:   string(pemEncodeCertificate(cert.Raw)),
		Key:    string(pemEncodePrivateKey(key)),
	}, nil
}

func issueCloudPlaneCertificate(caCert *x509.Certificate, caKey *rsa.PrivateKey, caPEM string, plan cloudPlaneInstallPlan) (tlsTemplateData, error) {
	dnsNames, ipAddresses := certificateNames(plan)
	cert, key, err := newLeafCertificate(caCert, caKey, "mini-cloud-cloud-plane-"+plan.Plane.Name, dnsNames, ipAddresses, false)
	if err != nil {
		return tlsTemplateData{}, err
	}
	return tlsTemplateData{
		CACert: caPEM,
		Cert:   string(pemEncodeCertificate(cert.Raw)),
		Key:    string(pemEncodePrivateKey(key)),
	}, nil
}

func cloudPlaneCertificateNeedsIssue(material tlsTemplateData, caPEM string, plan cloudPlaneInstallPlan) bool {
	if material.Cert == "" || material.Key == "" || material.CACert != caPEM {
		return true
	}
	block, _ := pem.Decode([]byte(material.Cert))
	if block == nil || block.Type != "CERTIFICATE" {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	dnsNames, ipAddresses := certificateNames(plan)
	for _, name := range dnsNames {
		if err := cert.VerifyHostname(name); err != nil {
			return true
		}
	}
	for _, ip := range ipAddresses {
		if err := cert.VerifyHostname(ip.String()); err != nil {
			return true
		}
	}
	return false
}

func certificateNames(plan cloudPlaneInstallPlan) ([]string, []net.IP) {
	names := map[string]bool{}
	ips := map[string]net.IP{}
	addHost := func(value string) {
		value = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "grpcs://"), "grpc://")
		host, _, err := net.SplitHostPort(value)
		if err == nil {
			value = host
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if ip := net.ParseIP(value); ip != nil {
			ips[ip.String()] = ip
			return
		}
		names[value] = true
	}
	addHost(plan.PrivateIP)
	addHost(plan.Output.Platform.Value.PrivateIP)
	addHost(plan.Output.Platform.Value.PublicIP)
	addHost(plan.Output.Platform.Value.SSHHost)
	addHost(plan.Output.InstallEnv.Value.PlatformPrivateIP)
	addHost(plan.Output.InstallEnv.Value.PlatformPublicIP)
	addHost(plan.GRPCEndpoint)

	dnsNames := make([]string, 0, len(names))
	for name := range names {
		dnsNames = append(dnsNames, name)
	}
	ipAddresses := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		ipAddresses = append(ipAddresses, ip)
	}
	return dnsNames, ipAddresses
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil || serial.Sign() == 0 {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}

func pemEncodeCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func pemEncodePrivateKey(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
