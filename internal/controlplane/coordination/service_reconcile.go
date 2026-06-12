package coordination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
)

func (c *ServiceController) reconcileService(ctx context.Context, item model.Service) error {
	switch {
	case item.Status.DesiredState == model.DesiredStateDeleted:
		return c.reconcileServiceDeletion(ctx, item)
	case item.Metadata.Generation > item.Status.Observed.ObservedGeneration || shouldReapplyDesiredSpec(item):
		return c.reconcileServiceDesiredSpec(ctx, item)
	default:
		return nil
	}
}

func (c *ServiceController) reconcileServiceDeletion(ctx context.Context, serviceItem model.Service) error {
	targetPlaneID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem.Metadata.Generation, err), nil)
		return errors.Join(err, statusErr)
	}

	deletePlanID := deletePlanID(serviceItem)
	deleteErr := c.dispatcher.DispatchDelete(ctx, targetPlaneID, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletePlanID)
	if deleteErr != nil && !errors.Is(deleteErr, errPlaneObjectNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem.Metadata.Generation, deleteErr), nil)
		return errors.Join(deleteErr, statusErr)
	}
	runStatus := deletingRunStatus(serviceItem, deletePlanID)
	return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletePlanDispatchedStatus(serviceItem.Metadata.Generation, deletePlanID), &runStatus)
}

func (c *ServiceController) reconcileServiceDesiredSpec(ctx context.Context, serviceItem model.Service) error {
	targetPlaneID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err), nil)
		return errors.Join(err, statusErr)
	}
	return c.dispatchServiceRun(ctx, serviceItem, targetPlaneID)
}

func (c *ServiceController) dispatchServiceRun(ctx context.Context, serviceItem model.Service, planeID string) error {
	planID, err := c.dispatcher.DispatchRun(ctx, planeID, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err), nil)
		return errors.Join(err, statusErr)
	}
	runStatus := dispatchedRunStatus(serviceItem, planID)
	return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, planID), &runStatus)
}

func dispatchedRunStatus(serviceItem model.Service, planID string) model.RunStatus {
	runID := strings.TrimSpace(planID)
	if runID == "" {
		runID = fmt.Sprintf("%s-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
	}
	message := fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", runID)
	runStatus := model.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = model.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func deletingRunStatus(serviceItem model.Service, runID string) model.RunStatus {
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", runID)
	runStatus := model.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = model.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func (c *ServiceController) targetPlaneID(ctx context.Context, serviceItem model.Service) (string, error) {
	planeID := strings.TrimSpace(serviceItem.Spec.PlaneID)
	if planeID == "" {
		return "", errPlaneIDRequired
	}
	planeDetail, err := c.store.GetPlane(ctx, planeID)
	if err != nil {
		return "", err
	}
	if planeDetail.Status.Status == model.StatusOffline {
		return "", fmt.Errorf("%w: current status is %s", errPlaneNotReady, planeDetail.Status.Status)
	}
	return planeID, nil
}

func deletePlanID(serviceItem model.Service) string {
	return fmt.Sprintf("%s-delete-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
}

func (c *ServiceController) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status model.ServiceObservedStatus, run *model.RunStatus) error {
	input := store.UpdateServiceStatusInput{
		ObservedGeneration: status.ObservedGeneration,
		Phase:              status.Phase,
		Message:            status.Message,
		LastReconciledAt:   status.LastReconciledAt,
		Run:                run,
	}
	err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, input)
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func executionPlanDispatchedStatus(generation int64, planID string) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := "execution plan dispatched; waiting for node-agent execution result"
	if strings.TrimSpace(planID) != "" {
		message = fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", planID)
	}
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseProgressing,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func deletePlanDispatchedStatus(generation int64, planID string) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", planID)
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDeleting,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func failedServiceStatus(generation int64, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service reconcile failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDegraded,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func deletingFailureServiceStatus(generation int64, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service teardown failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDeleting,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func shouldReapplyDesiredSpec(item model.Service) bool {
	if item.Status.DesiredState != model.DesiredStateActive {
		return false
	}
	if item.Status.Observed.ObservedGeneration != item.Metadata.Generation {
		return false
	}
	switch item.Status.Observed.Phase {
	case model.PhasePending, model.PhaseDegraded:
		return true
	default:
		return false
	}
}
