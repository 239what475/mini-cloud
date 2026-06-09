package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/control/nodepool"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/store"
)

const (
	fastReconcileInterval = 2 * time.Second
	nodeHealthInterval    = 10 * time.Second
)

type ingressReconciler interface {
	ReconcileOnce(context.Context) error
}

type Manager struct {
	logger     *slog.Logger
	store      *store.Store
	ingress    ingressReconciler
	nodePool   *nodepool.Service
	staleAfter time.Duration

	wg sync.WaitGroup
}

func NewManager(logger *slog.Logger, stores *store.Store, driver nodeprovider.Driver, ingress ingressReconciler, cfg cloudplaneconfig.Config) *Manager {
	return &Manager{
		logger:     logger,
		store:      stores,
		ingress:    ingress,
		nodePool:   nodepool.NewService(logger, stores, driver, cfg),
		staleAfter: 3 * time.Minute,
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.startLoop(ctx, "node-health", nodeHealthInterval, m.reconcileNodeHealthOnce)
	m.startLoop(ctx, "ingress", fastReconcileInterval, m.reconcileIngressOnce)
	m.startLoop(ctx, "node-pool", fastReconcileInterval, m.nodePool.ReconcileOnce)
}

func (m *Manager) Wait() {
	m.wg.Wait()
}

func (m *Manager) startLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	m.wg.Go(func() {
		logger := m.logger.With("reconciler", name)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Warn("cloud-plane reconciler iteration failed", "error", err)
			}

			select {
			case <-ctx.Done():
				logger.Info("cloud-plane reconciler stopped", "reason", ctx.Err())
				return
			case <-ticker.C:
			}
		}
	})
}

func (m *Manager) reconcileNodeHealthOnce(ctx context.Context) error {
	result, err := m.store.UpdateStaleNodeHeartbeatState(ctx, m.staleAfter)
	if err != nil {
		return err
	}
	if len(result.NodesMarkedOffline) > 0 || len(result.ImpactedPlans) > 0 {
		m.logger.Warn("cloud-plane reconciled stale node heartbeats", "nodes_offline", len(result.NodesMarkedOffline), "impacted_plans", len(result.ImpactedPlans))
	}
	return nil
}

func (m *Manager) reconcileIngressOnce(ctx context.Context) error {
	if m.ingress == nil {
		return nil
	}
	return m.ingress.ReconcileOnce(ctx)
}
