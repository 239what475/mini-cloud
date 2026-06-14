package ops

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

type certificateBundle struct {
	CA           tlsTemplateData
	ControlPlane tlsTemplateData
	CloudPlanes  map[string]tlsTemplateData
}

func buildCertificateBundle(plans []cloudPlaneInstallPlan) (certificateBundle, error) {
	caCert, caKey, err := newCertificateAuthority()
	if err != nil {
		return certificateBundle{}, err
	}
	controlCert, controlKey, err := newLeafCertificate(caCert, caKey, "mini-cloud-control-plane", nil, nil, true)
	if err != nil {
		return certificateBundle{}, err
	}
	out := certificateBundle{
		CA: tlsTemplateData{
			CACert: string(pemEncodeCertificate(caCert.Raw)),
			Cert:   string(pemEncodeCertificate(caCert.Raw)),
			Key:    string(pemEncodePrivateKey(caKey)),
		},
		ControlPlane: tlsTemplateData{
			CACert: string(pemEncodeCertificate(caCert.Raw)),
			Cert:   string(pemEncodeCertificate(controlCert.Raw)),
			Key:    string(pemEncodePrivateKey(controlKey)),
		},
		CloudPlanes: make(map[string]tlsTemplateData, len(plans)),
	}
	for _, plan := range plans {
		dnsNames, ipAddresses := certificateNames(plan)
		cert, key, err := newLeafCertificate(caCert, caKey, "mini-cloud-cloud-plane-"+plan.Plane.Name, dnsNames, ipAddresses, false)
		if err != nil {
			return certificateBundle{}, err
		}
		out.CloudPlanes[plan.Plane.Name] = tlsTemplateData{
			CACert: string(pemEncodeCertificate(caCert.Raw)),
			Cert:   string(pemEncodeCertificate(cert.Raw)),
			Key:    string(pemEncodePrivateKey(key)),
		}
	}
	return out, nil
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
