// Package caddy applies cloud-plane ingress routes to an external Caddy process.
package caddy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

const requestTimeout = 15 * time.Second

type Config struct {
	ListenHTTPAddr       string
	ArtifactListenAddr   string
	ArtifactDocumentRoot string
	AdminURL             string
}

type Sink struct {
	logger                 *slog.Logger
	cfg                    Config
	httpClient             *http.Client
	lastAppliedFingerprint string
}

func NewSink(logger *slog.Logger, cfg Config) *Sink {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sink{
		logger:     logger,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (s *Sink) SyncRoutes(ctx context.Context, routes []cloudmodel.Route) error {
	config, err := buildConfig(s.cfg.ListenHTTPAddr, s.cfg.ArtifactListenAddr, s.cfg.ArtifactDocumentRoot, s.cfg.AdminURL, routes)
	if err != nil {
		return err
	}
	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode caddy config: %w", err)
	}
	fingerprint := contentFingerprint(body)
	if fingerprint == s.lastAppliedFingerprint {
		return nil
	}
	if err := s.load(ctx, body); err != nil {
		return err
	}
	s.lastAppliedFingerprint = fingerprint
	s.logger.Info("cloud-plane applied ingress caddy config", "routes", len(routes), "admin_url", s.cfg.AdminURL)
	return nil
}

func (s *Sink) load(ctx context.Context, body []byte) error {
	adminURL, err := parseAdminURL(s.cfg.AdminURL)
	if err != nil {
		return err
	}
	loadURL := adminURL.JoinPath("load")
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, loadURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create caddy load request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("load caddy config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("load caddy config: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

type caddyConfig struct {
	Admin caddyAdmin `json:"admin"`
	Apps  caddyApps  `json:"apps"`
}

type caddyAdmin struct {
	Listen string `json:"listen"`
}

type caddyApps struct {
	HTTP caddyHTTPApp `json:"http"`
}

type caddyHTTPApp struct {
	Servers map[string]caddyHTTPServer `json:"servers"`
}

type caddyHTTPServer struct {
	Listen         []string         `json:"listen"`
	AutomaticHTTPS automaticHTTPS   `json:"automatic_https"`
	Routes         []caddyHTTPRoute `json:"routes"`
}

type automaticHTTPS struct {
	Disable bool `json:"disable"`
}

type caddyHTTPRoute struct {
	Match  []caddyHTTPMatcher `json:"match,omitempty"`
	Handle []caddyHTTPHandler `json:"handle"`
}

type caddyHTTPMatcher struct {
	Host []string `json:"host,omitempty"`
}

type caddyHTTPHandler struct {
	Handler    string          `json:"handler"`
	StatusCode int             `json:"status_code,omitempty"`
	Body       string          `json:"body,omitempty"`
	Root       string          `json:"root,omitempty"`
	Upstreams  []caddyUpstream `json:"upstreams,omitempty"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}

func buildConfig(listenHTTPAddr string, artifactListenAddr string, artifactDocumentRoot string, adminURL string, routes []cloudmodel.Route) (caddyConfig, error) {
	listenHTTPAddr = strings.TrimSpace(listenHTTPAddr)
	if listenHTTPAddr == "" {
		return caddyConfig{}, fmt.Errorf("caddy listen HTTP address is required")
	}
	artifactListenAddr = strings.TrimSpace(artifactListenAddr)
	if artifactListenAddr == "" {
		return caddyConfig{}, fmt.Errorf("caddy artifact listen address is required")
	}
	artifactDocumentRoot = strings.TrimSpace(artifactDocumentRoot)
	if artifactDocumentRoot == "" {
		return caddyConfig{}, fmt.Errorf("caddy artifact document root is required")
	}
	parsedAdminURL, err := parseAdminURL(adminURL)
	if err != nil {
		return caddyConfig{}, err
	}
	caddyRoutes := make([]caddyHTTPRoute, 0, len(routes)+1)
	for _, route := range routes {
		host := strings.TrimSpace(route.Host)
		if host == "" {
			continue
		}
		handler := serviceUnavailableHandler()
		if len(route.Backends) > 0 {
			upstreams := make([]caddyUpstream, 0, len(route.Backends))
			for _, backend := range route.Backends {
				backend = strings.TrimSpace(backend)
				if backend == "" {
					continue
				}
				upstreams = append(upstreams, caddyUpstream{Dial: backend})
			}
			if len(upstreams) > 0 {
				handler = caddyHTTPHandler{Handler: "reverse_proxy", Upstreams: upstreams}
			}
		}
		caddyRoutes = append(caddyRoutes, caddyHTTPRoute{
			Match:  []caddyHTTPMatcher{{Host: []string{host}}},
			Handle: []caddyHTTPHandler{handler},
		})
	}
	caddyRoutes = append(caddyRoutes, caddyHTTPRoute{Handle: []caddyHTTPHandler{notFoundHandler()}})
	return caddyConfig{
		Admin: caddyAdmin{Listen: parsedAdminURL.Host},
		Apps: caddyApps{
			HTTP: caddyHTTPApp{
				Servers: map[string]caddyHTTPServer{
					"mini_cloud_ingress": {
						Listen:         []string{listenHTTPAddr},
						AutomaticHTTPS: automaticHTTPS{Disable: true},
						Routes:         caddyRoutes,
					},
					"mini_cloud_artifacts": {
						Listen:         []string{artifactListenAddr},
						AutomaticHTTPS: automaticHTTPS{Disable: true},
						Routes: []caddyHTTPRoute{{
							Handle: []caddyHTTPHandler{{
								Handler: "file_server",
								Root:    artifactDocumentRoot,
							}},
						}},
					},
				},
			},
		},
	}, nil
}

func serviceUnavailableHandler() caddyHTTPHandler {
	return caddyHTTPHandler{
		Handler:    "static_response",
		StatusCode: http.StatusServiceUnavailable,
		Body:       "service backend is not ready",
	}
}

func notFoundHandler() caddyHTTPHandler {
	return caddyHTTPHandler{
		Handler:    "static_response",
		StatusCode: http.StatusNotFound,
		Body:       "mini-cloud ingress route not found",
	}
}

func parseAdminURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("parse caddy admin URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("caddy admin URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("caddy admin URL must include host")
	}
	if parsed.Port() == "" {
		return nil, fmt.Errorf("caddy admin URL must include port")
	}
	return parsed, nil
}

func contentFingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
