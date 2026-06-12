package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
)

const (
	defaultReconcileInterval = 10 * time.Second
	defaultReconcileTimeout  = 30 * time.Second
)

var errPlaneNotReady = errors.New("plane is not ready")

type ServiceController struct {
	logger     *slog.Logger
	store      *store.Store
	dispatcher *serviceDispatcher
	interval   time.Duration
	timeout    time.Duration
	trigger    chan struct{}
}

func NewServiceController(logger *slog.Logger, stores *store.Store, southboundToken string) *ServiceController {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceController{
		logger:     logger,
		store:      stores,
		dispatcher: newServiceDispatcher(logger, stores, southboundToken),
		interval:   defaultReconcileInterval,
		timeout:    defaultReconcileTimeout,
		trigger:    make(chan struct{}, 1),
	}
}

func (c *ServiceController) Create(ctx context.Context, input store.CreateServiceInput) (model.Service, error) {
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return model.Service{}, err
	}
	c.triggerSoon()
	return created, nil
}

func (c *ServiceController) List(ctx context.Context) ([]model.Service, error) {
	return c.store.ListServices(ctx)
}

func (c *ServiceController) Get(ctx context.Context, serviceID string) (model.Service, error) {
	return c.store.GetService(ctx, serviceID)
}

func (c *ServiceController) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	c.triggerSoon()
	return updated, nil
}

func (c *ServiceController) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	c.triggerSoon()
	return deleting, nil
}

func (c *ServiceController) triggerSoon() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

func (c *ServiceController) Run(ctx context.Context) {
	c.reconcileAndLog(ctx, "initial")
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileAndLog(ctx, "periodic")
		case <-c.trigger:
			c.reconcileAndLog(ctx, "triggered")
		}
	}
}

func (c *ServiceController) reconcileServices(ctx context.Context) error {
	items, err := c.store.ListServices(ctx)
	if err != nil {
		return err
	}
	var reconcileErrs []error
	for _, item := range items {
		if err := c.reconcileService(ctx, item); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("service %s: %w", item.Metadata.ID, err))
		}
	}
	return errors.Join(reconcileErrs...)
}

func (c *ServiceController) reconcileAndLog(ctx context.Context, reason string) {
	if err := c.reconcileOnce(ctx); err != nil && c.logger != nil {
		c.logger.Error("service reconcile failed", "reason", reason, "error", err)
	}
}

func (c *ServiceController) reconcileOnce(ctx context.Context) error {
	reconcileCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		reconcileCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()
	return c.reconcileServices(reconcileCtx)
}
