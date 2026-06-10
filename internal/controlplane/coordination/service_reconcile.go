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
	assignedPlaneID := strings.TrimSpace(item.Status.Observed.AssignedPlaneID)
	hasAssignment := assignedPlaneID != ""

	switch {
	case item.Status.DesiredState == model.DesiredStateDeleted:
		return c.reconcileServiceDeletion(ctx, item, hasAssignment, assignedPlaneID)
	case !hasAssignment:
		return c.reconcileServiceWithoutAssignment(ctx, item)
	case item.Metadata.Generation > item.Status.Observed.ObservedGeneration || shouldReapplyDesiredSpec(item):
		return c.reconcileServiceDesiredSpec(ctx, item, assignedPlaneID)
	default:
		return nil
	}
}

func (c *ServiceController) reconcileServiceDeletion(ctx context.Context, serviceItem model.Service, hasAssignment bool, assignedPlaneID string) error {
	if !hasAssignment {
		if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
			!errors.Is(err, store.ErrServiceNotFound) &&
			!errors.Is(err, store.ErrServiceGenerationConflict) {
			return err
		}
		return nil
	}

	deletePlanID := deletePlanID(serviceItem)
	deleteErr := c.deploy.DeleteService(ctx, assignedPlaneID, deleteServiceInput{
		ServiceID:         serviceItem.Metadata.ID,
		ServiceGeneration: serviceItem.Metadata.Generation,
		PlanID:            deletePlanID,
	})
	if deleteErr != nil && !errors.Is(deleteErr, errPlaneObjectNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem.Metadata.Generation, deleteErr))
		return errors.Join(deleteErr, statusErr)
	}
	runStatus := deletingRunStatus(serviceItem, deletePlanID)
	return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletePlanDispatchedStatus(serviceItem.Metadata.Generation, assignedPlaneID, deletePlanID), &runStatus)
}

func (c *ServiceController) reconcileServiceWithoutAssignment(ctx context.Context, serviceItem model.Service) error {
	targetPlaneID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return errors.Join(err, statusErr)
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, targetPlaneID, nil)
	return err
}

func (c *ServiceController) reconcileServiceDesiredSpec(ctx context.Context, serviceItem model.Service, assignedPlaneID string) error {
	targetPlaneID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return errors.Join(err, statusErr)
	}
	if strings.TrimSpace(assignedPlaneID) == targetPlaneID {
		_, err := c.applyServiceToAssignedPlane(ctx, serviceItem, assignedPlaneID)
		return err
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, targetPlaneID, &assignedPlaneID)
	return err
}

func (c *ServiceController) applyServiceToAssignedPlane(ctx context.Context, serviceItem model.Service, assignedPlaneID string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, assignedPlaneID, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return "", errors.Join(err, statusErr)
	}
	runStatus := dispatchedRunStatus(serviceItem, result)
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, result), &runStatus)
	return result.PlaneID, statusErr
}

func (c *ServiceController) applyServiceToPlane(ctx context.Context, serviceItem model.Service, planeID string, previousPlaneID *string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, planeID, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return "", errors.Join(err, statusErr)
	}

	if previousPlaneID != nil && shouldMoveAssignment(*previousPlaneID, result.PlaneID) {
		if err := c.deploy.DeleteService(ctx, *previousPlaneID, remoteDeleteInput(serviceItem, "move-old")); err != nil && !errors.Is(err, errPlaneObjectNotFound) {
			_ = c.deploy.DeleteService(ctx, result.PlaneID, remoteDeleteInput(serviceItem, "move-new"))
			statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, fmt.Errorf("delete old remote service after move: %w", err)))
			return "", errors.Join(err, statusErr)
		}
	}
	runStatus := dispatchedRunStatus(serviceItem, result)
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, result), &runStatus)
	return result.PlaneID, statusErr
}

func dispatchedRunStatus(serviceItem model.Service, result applyResult) model.RunStatus {
	runID := strings.TrimSpace(result.PlanID)
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
	if !planeDetail.Registration.Registered {
		return "", errPlaneApplyNotRegistered
	}
	if planeDetail.Status.Status == model.StatusOffline {
		return "", fmt.Errorf("%w: current status is %s", errPlaneNotReady, planeDetail.Status.Status)
	}
	return planeID, nil
}

func deletePlanID(serviceItem model.Service) string {
	return fmt.Sprintf("%s-delete-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
}

func remoteDeleteInput(serviceItem model.Service, reason string) deleteServiceInput {
	return deleteServiceInput{
		ServiceID:         serviceItem.Metadata.ID,
		ServiceGeneration: serviceItem.Metadata.Generation,
		PlanID:            fmt.Sprintf("%s-%s-g%d", serviceItem.Metadata.ID, reason, serviceItem.Metadata.Generation),
	}
}

func shouldMoveAssignment(currentPlaneID string, nextPlaneID string) bool {
	return strings.TrimSpace(currentPlaneID) != strings.TrimSpace(nextPlaneID)
}

func (c *ServiceController) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status model.ServiceObservedStatus, run ...*model.RunStatus) error {
	var nextRun *model.RunStatus
	if len(run) > 0 {
		nextRun = run[0]
	}
	input := store.UpdateServiceStatusInput{
		ObservedGeneration: status.ObservedGeneration,
		Phase:              status.Phase,
		Healthy:            status.Healthy,
		Message:            status.Message,
		LastReconciledAt:   status.LastReconciledAt,
		Run:                nextRun,
	}
	if strings.TrimSpace(status.AssignedPlaneID) != "" {
		input.AssignedPlaneID = &status.AssignedPlaneID
	}
	if strings.TrimSpace(status.RemoteStatus) != "" {
		input.RemoteStatus = &status.RemoteStatus
	}
	if strings.TrimSpace(status.RemoteMessage) != "" {
		input.RemoteMessage = &status.RemoteMessage
	}
	err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, input)
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func executionPlanDispatchedStatus(generation int64, result applyResult) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := "execution plan dispatched; waiting for node-agent execution result"
	if strings.TrimSpace(result.PlanID) != "" {
		message = fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", result.PlanID)
	}
	assignedPlaneID := result.PlaneID
	remoteStatus := "accepted"
	remoteMessage := message
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseProgressing,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    assignedPlaneID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func deletePlanDispatchedStatus(generation int64, planeID string, planID string) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", planID)
	remoteStatus := "deleting"
	remoteMessage := message
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDeleting,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    planeID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func failedServiceStatus(generation int64, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service reconcile failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func refreshFailureServiceStatus(generation int64, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service refresh failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDegraded,
		Healthy:            false,
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
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func isRemoteProgressing(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "starting", "deploying", "updating", "progressing":
		return true
	default:
		return false
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
