package serviceops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	domain "mini-cloud/internal/controlplane/domain"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/store"
)

func (c *Controller) reconcileService(ctx context.Context, item domain.Service) error {
	assignedPlaneID := strings.TrimSpace(item.Status.Observed.AssignedPlaneID)
	hasAssignment := assignedPlaneID != ""

	switch {
	case item.Status.DesiredState == domain.DesiredStateDeleted:
		return c.reconcileServiceDeletion(ctx, item, hasAssignment, assignedPlaneID)
	case !hasAssignment:
		return c.reconcileServiceWithoutAssignment(ctx, item)
	case item.Metadata.Generation > item.Status.Observed.ObservedGeneration || shouldReapplyDesiredSpec(item):
		return c.reconcileServiceDesiredSpec(ctx, item, assignedPlaneID)
	default:
		return nil
	}
}

func (c *Controller) reconcileServiceDeletion(ctx context.Context, serviceItem domain.Service, hasAssignment bool, assignedPlaneID string) error {
	if !hasAssignment {
		if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
			!errors.Is(err, store.ErrServiceNotFound) &&
			!errors.Is(err, store.ErrServiceGenerationConflict) {
			return err
		}
		return nil
	}

	deletePlanID := deletePlanID(serviceItem)
	deleteErr := c.deploy.DeleteService(ctx, assignedPlaneID, DeleteServiceInput{
		ServiceID:         serviceItem.Metadata.ID,
		ServiceGeneration: serviceItem.Metadata.Generation,
		PlanID:            deletePlanID,
	})
	if deleteErr != nil && !errors.Is(deleteErr, planeclient.ErrNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem.Metadata.Generation, deleteErr))
		return errors.Join(deleteErr, statusErr)
	}
	runStatus := deletingRunStatus(serviceItem, deletePlanID)
	return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletePlanDispatchedStatus(serviceItem.Metadata.Generation, assignedPlaneID, deletePlanID), &runStatus)
}

func (c *Controller) reconcileServiceWithoutAssignment(ctx context.Context, serviceItem domain.Service) error {
	targetPlaneID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return errors.Join(err, statusErr)
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, targetPlaneID, nil)
	return err
}

func (c *Controller) reconcileServiceDesiredSpec(ctx context.Context, serviceItem domain.Service, assignedPlaneID string) error {
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

func (c *Controller) applyServiceToAssignedPlane(ctx context.Context, serviceItem domain.Service, assignedPlaneID string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, assignedPlaneID, toDeployApplyInput(serviceItem))
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return "", errors.Join(err, statusErr)
	}
	runStatus := dispatchedRunStatus(serviceItem, result)
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, result), &runStatus)
	return result.PlaneID, statusErr
}

func (c *Controller) applyServiceToPlane(ctx context.Context, serviceItem domain.Service, planeID string, previousPlaneID *string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, planeID, toDeployApplyInput(serviceItem))
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return "", errors.Join(err, statusErr)
	}

	if previousPlaneID != nil && shouldMoveAssignment(*previousPlaneID, result.PlaneID) {
		if err := c.deploy.DeleteService(ctx, *previousPlaneID, remoteDeleteInput(serviceItem, "move-old")); err != nil && !errors.Is(err, planeclient.ErrNotFound) {
			_ = c.deploy.DeleteService(ctx, result.PlaneID, remoteDeleteInput(serviceItem, "move-new"))
			statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, fmt.Errorf("delete old remote service after move: %w", err)))
			return "", errors.Join(err, statusErr)
		}
	}
	runStatus := dispatchedRunStatus(serviceItem, result)
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, result), &runStatus)
	return result.PlaneID, statusErr
}

func dispatchedRunStatus(serviceItem domain.Service, result ApplyResult) domain.RunStatus {
	runID := strings.TrimSpace(result.PlanID)
	if runID == "" {
		runID = fmt.Sprintf("%s-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
	}
	message := fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", runID)
	runStatus := domain.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = domain.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func deletingRunStatus(serviceItem domain.Service, runID string) domain.RunStatus {
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", runID)
	runStatus := domain.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = domain.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func (c *Controller) targetPlaneID(ctx context.Context, serviceItem domain.Service) (string, error) {
	planeID := strings.TrimSpace(serviceItem.Spec.PlaneID)
	if planeID == "" {
		return "", domain.ErrPlaneIDRequired
	}
	planeDetail, err := c.store.GetPlane(ctx, planeID)
	if err != nil {
		return "", err
	}
	if !planeDetail.Registration.Registered {
		return "", ErrPlaneNotRegistered
	}
	if planeDetail.Status.Status != domain.StatusReady {
		return "", fmt.Errorf("%w: current status is %s", ErrPlaneNotReady, planeDetail.Status.Status)
	}
	return planeID, nil
}

func deletePlanID(serviceItem domain.Service) string {
	return fmt.Sprintf("%s-delete-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
}

func remoteDeleteInput(serviceItem domain.Service, reason string) DeleteServiceInput {
	return DeleteServiceInput{
		ServiceID:         serviceItem.Metadata.ID,
		ServiceGeneration: serviceItem.Metadata.Generation,
		PlanID:            fmt.Sprintf("%s-%s-g%d", serviceItem.Metadata.ID, reason, serviceItem.Metadata.Generation),
	}
}

func shouldMoveAssignment(currentPlaneID string, nextPlaneID string) bool {
	return strings.TrimSpace(currentPlaneID) != strings.TrimSpace(nextPlaneID)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func toDeployApplyInput(serviceItem domain.Service) ApplyServiceInput {
	return ApplyServiceInput{
		Metadata: ServiceMetadata{
			ID:          serviceItem.Metadata.ID,
			Name:        serviceItem.Metadata.Name,
			DisplayName: serviceItem.Metadata.DisplayName,
			Generation:  serviceItem.Metadata.Generation,
		},
		Spec: ServiceSpec{
			InstanceClass:        serviceItem.Spec.InstanceClass,
			Exposure:             serviceItem.Spec.Exposure,
			Image:                serviceItem.Spec.Image,
			Command:              append([]string(nil), serviceItem.Spec.Command...),
			Args:                 append([]string(nil), serviceItem.Spec.Args...),
			DefaultPort:          serviceItem.Spec.DefaultPort,
			ReadinessPath:        serviceItem.Spec.ReadinessPath,
			Env:                  cloneStringMap(serviceItem.Spec.Env),
			SecretEnv:            cloneStringMap(serviceItem.Spec.SecretEnv),
			RegistryCredentialID: serviceItem.Spec.RegistryCredentialID,
			Files:                projectedfile.CloneFiles(serviceItem.Spec.Files),
		},
	}
}

func (c *Controller) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status domain.Status, run ...*domain.RunStatus) error {
	var nextRun *domain.RunStatus
	if len(run) > 0 {
		nextRun = run[0]
	}
	input := domain.ServiceUpdateStatusInput{
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
	_, err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, input)
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func executionPlanDispatchedStatus(generation int64, result ApplyResult) domain.Status {
	now := time.Now().UTC()
	message := "execution plan dispatched; waiting for node-agent execution result"
	if strings.TrimSpace(result.PlanID) != "" {
		message = fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", result.PlanID)
	}
	assignedPlaneID := result.PlaneID
	remoteStatus := "accepted"
	remoteMessage := message
	return domain.Status{
		ObservedGeneration: generation,
		Phase:              domain.PhaseProgressing,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    assignedPlaneID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func deletePlanDispatchedStatus(generation int64, planeID string, planID string) domain.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", planID)
	remoteStatus := "deleting"
	remoteMessage := message
	return domain.Status{
		ObservedGeneration: generation,
		Phase:              domain.PhaseDeleting,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    planeID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func failedServiceStatus(generation int64, err error) domain.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service reconcile failed: %v", err)
	return domain.Status{
		ObservedGeneration: generation,
		Phase:              domain.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func refreshFailureServiceStatus(generation int64, err error) domain.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service refresh failed: %v", err)
	return domain.Status{
		ObservedGeneration: generation,
		Phase:              domain.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func deletingFailureServiceStatus(generation int64, err error) domain.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service teardown failed: %v", err)
	return domain.Status{
		ObservedGeneration: generation,
		Phase:              domain.PhaseDeleting,
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

func shouldReapplyDesiredSpec(item domain.Service) bool {
	if item.Status.DesiredState != domain.DesiredStateActive {
		return false
	}
	if item.Status.Observed.ObservedGeneration != item.Metadata.Generation {
		return false
	}
	switch item.Status.Observed.Phase {
	case domain.PhasePending, domain.PhaseDegraded:
		return true
	default:
		return false
	}
}
