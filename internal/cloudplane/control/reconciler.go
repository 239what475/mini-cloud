package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/store"
)

const (
	fastReconcileInterval = 2 * time.Second
	nodeHealthInterval    = 10 * time.Second
)

type Reconciler struct {
	logger     *slog.Logger
	store      *store.Store
	ingress    *ingressReconciler
	nodes      *nodeReconciler
	staleAfter time.Duration

	wg sync.WaitGroup
}

func NewReconciler(logger *slog.Logger, stores *store.Store, driver nodeprovider.Driver, localIngress RouteSink, frontDoor RouteSink, cfg cloudplaneconfig.Config) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		logger:     logger,
		store:      stores,
		ingress:    newIngressReconciler(logger, stores, cfg, localIngress, frontDoor),
		nodes:      newNodeReconciler(logger, stores, driver, cfg),
		staleAfter: 3 * time.Minute,
	}
}

func (m *Reconciler) Start(ctx context.Context) {
	m.startLoop(ctx, "node-health", nodeHealthInterval, m.reconcileNodeHealthOnce)
	m.startLoop(ctx, "ingress", fastReconcileInterval, m.reconcileIngressOnce)
	m.startLoop(ctx, "nodes", fastReconcileInterval, m.nodes.reconcileOnce)
}

func (m *Reconciler) Wait() {
	m.wg.Wait()
}

func (m *Reconciler) startLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
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

func (m *Reconciler) reconcileNodeHealthOnce(ctx context.Context) error {
	result, err := m.store.MarkStaleNodeHeartbeatsOffline(ctx, m.staleAfter)
	if err != nil {
		return err
	}
	if result.OfflineNodes > 0 || result.FailedExecutions > 0 {
		m.logger.Warn("cloud-plane reconciled stale node heartbeats", "nodes_offline", result.OfflineNodes, "failed_executions", result.FailedExecutions)
	}
	return nil
}

func (m *Reconciler) reconcileIngressOnce(ctx context.Context) error {
	if m.ingress == nil {
		return nil
	}
	return m.ingress.reconcileOnce(ctx)
}
