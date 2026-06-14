package transport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type TLSMaterial struct {
	CACert string
	Cert   string
	Key    string
}

func ClientTLSConfig(material TLSMaterial, serverName string) (*tls.Config, error) {
	caPool, err := certPool(material.CACert)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    caPool,
		ServerName: tlsServerName(serverName),
	}
	if strings.TrimSpace(material.Cert) != "" || strings.TrimSpace(material.Key) != "" {
		cert, err := tls.X509KeyPair([]byte(material.Cert), []byte(material.Key))
		if err != nil {
			return nil, fmt.Errorf("parse client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func tlsServerName(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}

func ServerTLSConfig(material TLSMaterial, requireClientCert bool) (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(material.Cert), []byte(material.Key))
	if err != nil {
		return nil, fmt.Errorf("parse server certificate: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if requireClientCert {
		clientCAs, err := certPool(material.CACert)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = clientCAs
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	} else if strings.TrimSpace(material.CACert) != "" {
		clientCAs, err := certPool(material.CACert)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = clientCAs
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return cfg, nil
}

func HasVerifiedClientCertificate(p *peer.Peer) bool {
	if p == nil || p.AuthInfo == nil {
		return false
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	return ok && len(tlsInfo.State.VerifiedChains) > 0
}

func certPool(caCert string) (*x509.CertPool, error) {
	trimmed := strings.TrimSpace(caCert)
	if trimmed == "" {
		return nil, fmt.Errorf("ca certificate is required")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(trimmed + "\n")) {
		return nil, fmt.Errorf("parse ca certificate")
	}
	return pool, nil
}
