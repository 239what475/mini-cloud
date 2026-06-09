// Package ingress 管理 cloud-plane 外置 ingress 数据面收敛。
package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

// storeReader 定义 ingress controller 构造 route snapshot 需要的本地状态读取能力。
type storeReader interface {
	// ListIngressRouteSources 汇总当前 public service 的 running execution 后端。
	ListIngressRouteSources(context.Context) ([]cloudmodel.RouteSource, error)
	// GetNode 按节点 ID 读取节点。
	GetNode(context.Context, string) (cloudmodel.Node, error)
}

// routeSink 定义外置 ingress 数据面应用路由快照的能力。
type routeSink interface {
	// Apply 将本轮路由快照应用到具体 ingress 数据面实现。
	Apply(context.Context, []cloudmodel.Route) error
}

// Controller 负责把本地 service 运行态转换为外置 ingress 路由快照。
type Controller struct {
	// logger 记录 ingress 路由构建和应用过程。
	logger *slog.Logger
	// store 提供 service execution 和 node 查询能力。
	store storeReader
	// cfg 是已校验的 cloud-plane 配置。
	cfg cloudplaneconfig.Config
	// sink 应用路由快照，具体实现可以是 Caddy Admin API、其它远端 API 或 no-op。
	sink routeSink
}

var dnsLabelCleaner = regexp.MustCompile(`[^a-z0-9-]+`)

// NewController 构造外置 ingress 控制器。
// 参数说明：logger 记录后台日志；stores 读取本地状态；cfg 提供 ingress 策略；sink 应用路由快照。
func NewController(logger *slog.Logger, stores storeReader, cfg cloudplaneconfig.Config, sink routeSink) *Controller {
	return &Controller{logger: logger, store: stores, cfg: cfg, sink: sink}
}

// ReconcileOnce 在启用 ingress 时构建当前 public service 路由快照，并交给外置数据面实现应用。
// 参数说明：ctx 控制数据库读取和下游 sink 应用生命周期。
func (c *Controller) ReconcileOnce(ctx context.Context) error {
	// 未启用 ingress 时不构建路由、不触碰外置数据面，保持纯 gRPC plane 的最小运行形态。
	if strings.TrimSpace(c.cfg.Ingress.BaseDomain) == "" {
		return nil
	}
	// 构建 public service 的 observed route snapshot；失败时终止本轮，下一轮 reconciler 继续重试。
	routes, err := c.buildRoutes(ctx)
	if err != nil {
		return err
	}
	// sink 代表具体数据面应用方式；未注入 sink 是组装错误，直接返回明确错误。
	if c.sink == nil {
		return fmt.Errorf("ingress sink is required when ingress is enabled")
	}
	// 将路由快照交给下游实现；例如 Caddy sink 会通过 Admin API 加载结构化配置。
	if err := c.sink.Apply(ctx, routes); err != nil {
		return err
	}
	c.logger.Debug("cloud-plane reconciled ingress routes", "routes", len(routes))
	return nil
}

// buildRoutes 根据 public service 当前 running execution 构造 ingress 路由。
// 参数说明：ctx 控制本地 store 查询生命周期。
func (c *Controller) buildRoutes(ctx context.Context) ([]cloudmodel.Route, error) {
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
		// 离线、draining 或 not_ready 节点上的历史 running execution 不应继续进入入口配置。
		// schedulable 只控制是否接收新调度，不代表已有 backend 不可服务，因此这里不按它过滤。
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

// managedHost 生成 public service 唯一的托管入口域名。
func (c *Controller) managedHost(serviceName string) string {
	return fmt.Sprintf("%s.%s", dnsLabel(serviceName), strings.Trim(strings.TrimSpace(c.cfg.Ingress.BaseDomain), "."))
}

// dnsLabel 将 service 名称转换为保守 DNS label。
func dnsLabel(value string) string {
	label := strings.ToLower(strings.TrimSpace(value))
	label = dnsLabelCleaner.ReplaceAllString(label, "-")
	label = strings.Trim(label, "-")
	if label == "" {
		return "unnamed"
	}
	if len(label) > 63 {
		label = strings.Trim(label[:63], "-")
		if label == "" {
			return "unnamed"
		}
	}
	return label
}
