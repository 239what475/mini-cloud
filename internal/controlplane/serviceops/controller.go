package serviceops

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

var ErrPlaneNotReady = errors.New("plane is not ready")

type executionPlanManager interface {
	ApplyService(context.Context, string, model.Service) (ApplyResult, error)
	DeleteService(context.Context, string, DeleteServiceInput) error
}

type serviceStore interface {
	CreateService(context.Context, store.CreateServiceInput) (model.Service, error)
	ListServices(context.Context) ([]model.Service, error)
	GetService(context.Context, string) (model.Service, error)
	UpdateService(context.Context, string, store.UpdateServiceInput) (model.Service, error)
	MarkServiceDeletionRequested(context.Context, string) (model.Service, error)
	UpdateServiceStatus(context.Context, string, store.UpdateServiceStatusInput) (model.Service, error)
	UpdateServiceStatusForGeneration(context.Context, string, int64, store.UpdateServiceStatusInput) (model.Service, error)
	DeleteService(context.Context, string) error
	DeleteServiceForGeneration(context.Context, string, int64) error
	GetPlane(context.Context, string) (model.PlaneDetail, error)
}

type Controller struct {
	logger   *slog.Logger
	store    serviceStore
	deploy   executionPlanManager
	interval time.Duration
	timeout  time.Duration
	trigger  chan struct{}
}

func New(logger *slog.Logger, stores serviceStore, deploySvc executionPlanManager) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		logger:   logger,
		store:    stores,
		deploy:   deploySvc,
		interval: defaultReconcileInterval,
		timeout:  defaultReconcileTimeout,
		trigger:  make(chan struct{}, 1),
	}
}

func (c *Controller) SetReconcileTimeout(timeout int) {
	if c == nil || timeout <= 0 {
		return
	}
	c.timeout = time.Duration(timeout) * time.Second
}

func (c *Controller) Create(ctx context.Context, input store.CreateServiceInput) (model.Service, error) {
	if err := c.validateConfigured(); err != nil {
		return model.Service{}, err
	}
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	return c.store.GetService(ctx, created.Metadata.ID)
}

func (c *Controller) List(ctx context.Context) ([]model.Service, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("service controller is not configured")
	}
	return c.store.ListServices(ctx)
}

func (c *Controller) Get(ctx context.Context, serviceID string) (model.Service, error) {
	if c == nil || c.store == nil {
		return model.Service{}, fmt.Errorf("service controller is not configured")
	}
	return c.store.GetService(ctx, serviceID)
}

func (c *Controller) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	if err := c.validateConfigured(); err != nil {
		return model.Service{}, err
	}
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	return c.store.GetService(ctx, updated.Metadata.ID)
}

func (c *Controller) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	if err := c.validateConfigured(); err != nil {
		return model.Service{}, err
	}
	if _, err := c.store.GetService(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	reloaded, err := c.store.GetService(ctx, deleting.Metadata.ID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return deleting, nil
		}
		return model.Service{}, err
	}
	return reloaded, nil
}

func (c *Controller) Trigger() {
	if c == nil {
		return
	}
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

func (c *Controller) Run(ctx context.Context) {
	if c == nil {
		return
	}
	c.runOnce(ctx, "initial")
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx, "periodic")
		case <-c.trigger:
			c.runOnce(ctx, "triggered")
		}
	}
}

func (c *Controller) ReconcileOnce(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.reconcileWithTimeout(ctx)
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if err := c.validateConfigured(); err != nil {
		return err
	}
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

func (c *Controller) validateConfigured() error {
	if c == nil || c.store == nil || c.deploy == nil {
		return fmt.Errorf("service controller is not configured")
	}
	return nil
}

func (c *Controller) runOnce(ctx context.Context, reason string) {
	if err := c.reconcileWithTimeout(ctx); err != nil && c.logger != nil {
		c.logger.Error("service reconcile failed", "reason", reason, "error", err)
	}
}

func (c *Controller) reconcileWithTimeout(ctx context.Context) error {
	reconcileCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		reconcileCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()
	return c.Reconcile(reconcileCtx)
}
