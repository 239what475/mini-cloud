package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

type storeReader interface {
	ListIngressRouteSources(context.Context) ([]cloudmodel.RouteSource, error)
	GetNode(context.Context, string) (cloudmodel.Node, error)
}

type RouteSink interface {
	Apply(context.Context, []cloudmodel.Route) error
}

type ingressReconciler struct {
	logger        *slog.Logger
	store         storeReader
	cfg           cloudplaneconfig.Config
	localSink     RouteSink
	frontDoorSink RouteSink
}

func newIngressReconciler(logger *slog.Logger, stores storeReader, cfg cloudplaneconfig.Config, localSink RouteSink, frontDoorSink RouteSink) *ingressReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ingressReconciler{logger: logger, store: stores, cfg: cfg, localSink: localSink, frontDoorSink: frontDoorSink}
}

func (c *ingressReconciler) reconcileOnce(ctx context.Context) error {
	caddyEnabled := strings.TrimSpace(c.cfg.Ingress.CaddyAdminURL) != ""
	serviceIngressEnabled := strings.TrimSpace(c.cfg.Ingress.BaseDomain) != ""
	if !caddyEnabled && !serviceIngressEnabled {
		return nil
	}

	var routes []cloudmodel.Route
	if serviceIngressEnabled {
		var err error
		routes, err = c.buildRoutes(ctx)
		if err != nil {
			return err
		}
	}

	if c.localSink == nil {
		return fmt.Errorf("caddy route sink is required")
	}
	if err := c.localSink.Apply(ctx, routes); err != nil {
		return err
	}
	if serviceIngressEnabled && c.frontDoorSink != nil {
		if err := c.frontDoorSink.Apply(ctx, routes); err != nil {
			return err
		}
	}
	c.logger.Debug("cloud-plane reconciled ingress routes", "routes", len(routes))
	return nil
}

func (c *ingressReconciler) buildRoutes(ctx context.Context) ([]cloudmodel.Route, error) {
	sources, err := c.store.ListIngressRouteSources(ctx)
	if err != nil {
		return nil, err
	}
	hosts := make([]string, 0)
	backendsByHost := make(map[string][]string)
	for _, source := range sources {
		host := strings.TrimSpace(source.Host)
		if host == "" {
			continue
		}
		if _, ok := backendsByHost[host]; !ok {
			hosts = append(hosts, host)
			backendsByHost[host] = nil
		}
		if !source.HasBackend || source.HostPort <= 0 {
			continue
		}
		nodeItem, err := c.store.GetNode(ctx, source.NodeID)
		if err != nil {
			if errors.Is(err, store.ErrNodeNotFound) {
				continue
			}
			return nil, err
		}
		if nodeItem.Status != cloudmodel.StatusReady {
			continue
		}
		privateIP := strings.TrimSpace(nodeItem.PrivateIP)
		if privateIP == "" {
			continue
		}
		backendsByHost[host] = append(backendsByHost[host], net.JoinHostPort(privateIP, fmt.Sprintf("%d", source.HostPort)))
	}

	routes := make([]cloudmodel.Route, 0, len(hosts))
	for _, host := range hosts {
		backends := backendsByHost[host]
		routes = append(routes, cloudmodel.Route{Host: host, Backends: backends})
	}
	return routes, nil
}
