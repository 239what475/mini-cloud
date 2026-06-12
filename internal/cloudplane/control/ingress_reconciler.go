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
	serviceNames := make([]string, 0)
	backendsByService := make(map[string][]string)
	for _, source := range sources {
		serviceName := strings.TrimSpace(source.ServiceName)
		if serviceName == "" {
			continue
		}
		if _, ok := backendsByService[serviceName]; !ok {
			serviceNames = append(serviceNames, serviceName)
			backendsByService[serviceName] = nil
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
		backendsByService[serviceName] = append(backendsByService[serviceName], net.JoinHostPort(privateIP, fmt.Sprintf("%d", source.HostPort)))
	}

	routes := make([]cloudmodel.Route, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		backends := backendsByService[serviceName]
		routes = append(routes, cloudmodel.Route{Host: c.managedHost(serviceName), Backends: backends})
	}
	return routes, nil
}

func (c *ingressReconciler) managedHost(serviceName string) string {
	return fmt.Sprintf("%s.%s", strings.TrimSpace(serviceName), strings.Trim(strings.TrimSpace(c.cfg.Ingress.BaseDomain), "."))
}
