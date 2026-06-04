package servicecontroller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"mini-cloud/internal/controlplane/deploy"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planeselector"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/store"
)

const (
	defaultReconcileInterval = 10 * time.Second
	defaultReconcileTimeout  = 30 * time.Second
)

var ErrNoEligiblePlacement = errors.New("no eligible plane matched the requested provider/region/capacity")

type View struct {
	Service   controlservice.Service           `json:"service"`
	Placement *controlservice.ServicePlacement `json:"placement,omitempty"`
	Plane     *plane.Detail                    `json:"plane,omitempty"`
}

type planeSelector interface {
	PreviewSelection(context.Context, planeselector.SelectionInput) (planeselector.SelectionResult, error)
}

type deploymentManager interface {
	ApplyService(context.Context, string, deploy.ApplyServiceInput) (deploy.ApplyResult, error)
	DeleteService(context.Context, string, string) error
}

type serviceStore interface {
	CreateService(context.Context, controlservice.CreateInput) (controlservice.Service, error)
	ListServices(context.Context) ([]controlservice.Service, error)
	GetService(context.Context, string) (controlservice.Service, error)
	UpdateService(context.Context, string, controlservice.UpdateInput) (controlservice.Service, error)
	MarkServiceDeletionRequested(context.Context, string) (controlservice.Service, error)
	UpdateServiceStatus(context.Context, string, controlservice.UpdateStatusInput) (controlservice.Service, error)
	UpdateServiceStatusForGeneration(context.Context, string, int64, controlservice.UpdateStatusInput) (controlservice.Service, error)
	DeleteService(context.Context, string) error
	DeleteServiceForGeneration(context.Context, string, int64) error
	GetServicePlacement(context.Context, string) (controlservice.ServicePlacement, error)
	UpsertServicePlacement(context.Context, controlservice.ServicePlacement) (controlservice.ServicePlacement, error)
	UpsertServicePlacementForGeneration(context.Context, controlservice.ServicePlacement, int64, bool) (controlservice.ServicePlacement, error)
	DeleteServicePlacement(context.Context, string) error
	DeleteServicePlacementForGeneration(context.Context, string, int64) error
	GetPlane(context.Context, string) (plane.Detail, error)
}

type Controller struct {
	logger   *slog.Logger
	store    serviceStore
	selector planeSelector
	deploy   deploymentManager
	interval time.Duration
	timeout  time.Duration
	trigger  chan struct{}
}

func New(logger *slog.Logger, stores serviceStore, planner planeSelector, deploySvc deploymentManager) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		logger:   logger,
		store:    stores,
		selector: planner,
		deploy:   deploySvc,
		interval: defaultReconcileInterval,
		timeout:  defaultReconcileTimeout,
		trigger:  make(chan struct{}, 1),
	}
}

func (c *Controller) SetReconcileTimeout(timeout time.Duration) {
	if c == nil || timeout <= 0 {
		return
	}
	c.timeout = timeout
}

func (c *Controller) Create(ctx context.Context, input controlservice.CreateInput) (View, error) {
	if err := c.validateConfigured(); err != nil {
		return View{}, err
	}
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return View{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	reloaded, err := c.store.GetService(ctx, created.Metadata.ID)
	if err != nil {
		return View{}, err
	}
	return c.buildView(ctx, reloaded)
}

func (c *Controller) List(ctx context.Context) ([]View, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("service controller is not configured")
	}
	items, err := c.store.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(items))
	for _, item := range items {
		view, buildErr := c.buildView(ctx, item)
		if buildErr != nil {
			return nil, buildErr
		}
		out = append(out, view)
	}
	return out, nil
}

func (c *Controller) Get(ctx context.Context, serviceID string) (View, error) {
	if c == nil || c.store == nil {
		return View{}, fmt.Errorf("service controller is not configured")
	}
	item, err := c.store.GetService(ctx, serviceID)
	if err != nil {
		return View{}, err
	}
	return c.buildView(ctx, item)
}

func (c *Controller) Update(ctx context.Context, serviceID string, input controlservice.UpdateInput) (View, error) {
	if err := c.validateConfigured(); err != nil {
		return View{}, err
	}
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return View{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	reloaded, err := c.store.GetService(ctx, updated.Metadata.ID)
	if err != nil {
		return View{}, err
	}
	return c.buildView(ctx, reloaded)
}

func (c *Controller) Delete(ctx context.Context, serviceID string) (View, error) {
	if err := c.validateConfigured(); err != nil {
		return View{}, err
	}
	if _, err := c.store.GetService(ctx, serviceID); err != nil {
		return View{}, err
	}
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return View{}, err
	}
	if err := c.ReconcileOnce(ctx); err != nil {
		c.Trigger()
	}
	reloaded, err := c.store.GetService(ctx, deleting.Metadata.ID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return View{Service: deleting}, nil
		}
		return View{}, err
	}
	return c.buildView(ctx, reloaded)
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
	if c == nil || c.store == nil || c.selector == nil || c.deploy == nil {
		return fmt.Errorf("service controller is not configured")
	}
	return nil
}

func (c *Controller) buildView(ctx context.Context, item controlservice.Service) (View, error) {
	var placementRecord *controlservice.ServicePlacement
	placementItem, err := c.store.GetServicePlacement(ctx, item.Metadata.ID)
	switch {
	case err == nil:
		copyPlacement := placementItem
		placementRecord = &copyPlacement
	case errors.Is(err, store.ErrServicePlacementNotFound):
	default:
		return View{}, err
	}
	return View{
		Service:   item,
		Placement: placementRecord,
	}, nil
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
