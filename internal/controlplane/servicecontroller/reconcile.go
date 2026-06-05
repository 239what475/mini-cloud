package servicecontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/controlplane/deploy"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/planeselector"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/store"
)

func (c *Controller) reconcileService(ctx context.Context, item controlservice.Service) error {
	assignedPlaneID := strings.TrimSpace(item.Status.Observed.AssignedPlaneID)
	hasAssignment := assignedPlaneID != ""

	switch {
	case item.Status.DesiredState == controlservice.DesiredStateDeleted:
		return c.reconcileServiceDeletion(ctx, item, hasAssignment, assignedPlaneID)
	case !hasAssignment:
		return c.reconcileServiceWithoutAssignment(ctx, item)
	case item.Metadata.Generation > item.Status.Observed.ObservedGeneration || shouldReapplyDesiredSpec(item):
		return c.reconcileServiceDesiredSpec(ctx, item, assignedPlaneID)
	default:
		return nil
	}
}

func (c *Controller) reconcileServiceDeletion(ctx context.Context, serviceItem controlservice.Service, hasAssignment bool, assignedPlaneID string) error {
	if !hasAssignment {
		if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
			!errors.Is(err, store.ErrServiceNotFound) &&
			!errors.Is(err, store.ErrServiceGenerationConflict) {
			return err
		}
		return nil
	}

	deletePlanID := deletePlanID(serviceItem)
	deleteErr := c.deploy.DeleteService(ctx, assignedPlaneID, deploy.DeleteServiceInput{
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

func (c *Controller) reconcileServiceWithoutAssignment(ctx context.Context, serviceItem controlservice.Service) error {
	decision, err := c.selectPlane(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return errors.Join(err, statusErr)
	}
	if decision == nil {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, pendingPlaneSelectorStatus(serviceItem.Metadata.Generation, ErrNoEligibleAssignment))
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, *decision, nil)
	return err
}

func (c *Controller) reconcileServiceDesiredSpec(ctx context.Context, serviceItem controlservice.Service, assignedPlaneID string) error {
	if c.canReuseAssignment(ctx, serviceItem, assignedPlaneID) {
		_, err := c.applyServiceToAssignedPlane(ctx, serviceItem, assignedPlaneID)
		return err
	}
	decision, err := c.selectPlane(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return errors.Join(err, statusErr)
	}
	if decision == nil {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, pendingPlaneSelectorStatus(serviceItem.Metadata.Generation, ErrNoEligibleAssignment))
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, *decision, &assignedPlaneID)
	return err
}

func (c *Controller) applyServiceToAssignedPlane(ctx context.Context, serviceItem controlservice.Service, assignedPlaneID string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, assignedPlaneID, toDeployApplyInput(serviceItem))
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, err))
		return "", errors.Join(err, statusErr)
	}
	runStatus := dispatchedRunStatus(serviceItem, result)
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, result), &runStatus)
	return result.PlaneID, statusErr
}

func (c *Controller) applyServiceToPlane(ctx context.Context, serviceItem controlservice.Service, decision planeselector.Decision, previousPlaneID *string) (string, error) {
	result, err := c.deploy.ApplyService(ctx, decision.PlaneID, toDeployApplyInput(serviceItem))
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

func dispatchedRunStatus(serviceItem controlservice.Service, result deploy.ApplyResult) controlservice.RunStatus {
	runID := strings.TrimSpace(result.PlanID)
	if runID == "" {
		runID = fmt.Sprintf("%s-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
	}
	message := fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", runID)
	runStatus := controlservice.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = controlservice.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func deletingRunStatus(serviceItem controlservice.Service, runID string) controlservice.RunStatus {
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", runID)
	runStatus := controlservice.CloneRunStatus(serviceItem.Status.Run)
	runStatus.LatestRunID = runID
	runStatus.Phase = controlservice.RunPhaseDispatching
	runStatus.Message = message
	return runStatus
}

func (c *Controller) selectPlane(ctx context.Context, serviceItem controlservice.Service) (*planeselector.Decision, error) {
	result, err := c.selector.PreviewSelection(ctx, planeselector.SelectionInput{
		Provider:      serviceItem.Spec.Provider,
		Region:        serviceItem.Spec.Region,
		PinnedPlaneID: serviceItem.Spec.PinnedPlaneID,
		InstanceClass: serviceItem.Spec.InstanceClass,
	})
	if err != nil {
		return nil, err
	}
	return result.Decision, nil
}

func (c *Controller) canReuseAssignment(ctx context.Context, serviceItem controlservice.Service, assignedPlaneID string) bool {
	if strings.TrimSpace(assignedPlaneID) == "" {
		return false
	}
	plane, err := c.store.GetPlane(ctx, assignedPlaneID)
	if err != nil {
		return false
	}
	if strings.TrimSpace(serviceItem.Spec.PinnedPlaneID) != "" && plane.ID != serviceItem.Spec.PinnedPlaneID {
		return false
	}
	if plane.Provider != serviceItem.Spec.Provider || plane.Region != serviceItem.Spec.Region {
		return false
	}
	if !plane.Registration.Registered || !plane.Operation.AcceptingNewRuns() {
		return false
	}
	return true
}

func deletePlanID(serviceItem controlservice.Service) string {
	return fmt.Sprintf("%s-delete-g%d", serviceItem.Metadata.ID, serviceItem.Metadata.Generation)
}

func remoteDeleteInput(serviceItem controlservice.Service, reason string) deploy.DeleteServiceInput {
	return deploy.DeleteServiceInput{
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

func toDeployApplyInput(serviceItem controlservice.Service) deploy.ApplyServiceInput {
	return deploy.ApplyServiceInput{
		Metadata: deploy.ServiceMetadata{
			ID:          serviceItem.Metadata.ID,
			Name:        serviceItem.Metadata.Name,
			DisplayName: serviceItem.Metadata.DisplayName,
			Generation:  serviceItem.Metadata.Generation,
		},
		Spec: deploy.ServiceSpec{
			Region:               serviceItem.Spec.Region,
			InstanceClass:        serviceItem.Spec.InstanceClass,
			Exposure:             serviceItem.Spec.Exposure,
			Image:                serviceItem.Spec.Image,
			Command:              append([]string(nil), serviceItem.Spec.Command...),
			Args:                 append([]string(nil), serviceItem.Spec.Args...),
			DefaultPort:          serviceItem.Spec.DefaultPort,
			ReadinessPath:        serviceItem.Spec.ReadinessPath,
			Env:                  cloneStringMap(serviceItem.Spec.Env),
			ConfigSetID:          serviceItem.Spec.ConfigSetID,
			SecretSetID:          serviceItem.Spec.SecretSetID,
			RegistryCredentialID: serviceItem.Spec.RegistryCredentialID,
			ProjectedFiles:       projectedfile.CloneSpecs(serviceItem.Spec.ProjectedFiles),
		},
	}
}

func (c *Controller) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status controlservice.Status, run ...*controlservice.RunStatus) error {
	var nextRun *controlservice.RunStatus
	if len(run) > 0 {
		nextRun = run[0]
	}
	input := controlservice.UpdateStatusInput{
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

func executionPlanDispatchedStatus(generation int64, result deploy.ApplyResult) controlservice.Status {
	now := time.Now().UTC()
	message := "execution plan dispatched; waiting for node-agent execution result"
	if strings.TrimSpace(result.PlanID) != "" {
		message = fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", result.PlanID)
	}
	assignedPlaneID := result.PlaneID
	remoteStatus := "accepted"
	remoteMessage := message
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseProgressing,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    assignedPlaneID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func deletePlanDispatchedStatus(generation int64, planeID string, planID string) controlservice.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("delete execution plan %s dispatched; waiting for node-agent cleanup result", planID)
	remoteStatus := "deleting"
	remoteMessage := message
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseDeleting,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
		AssignedPlaneID:    planeID,
		RemoteStatus:       remoteStatus,
		RemoteMessage:      remoteMessage,
	}
}

func pendingPlaneSelectorStatus(generation int64, err error) controlservice.Status {
	now := time.Now().UTC()
	message := "service is waiting for an eligible plane"
	if err != nil {
		message = err.Error()
	}
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhasePending,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func failedServiceStatus(generation int64, err error) controlservice.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service reconcile failed: %v", err)
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func refreshFailureServiceStatus(generation int64, err error) controlservice.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service refresh failed: %v", err)
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		LastReconciledAt:   &now,
	}
}

func deletingFailureServiceStatus(generation int64, err error) controlservice.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service teardown failed: %v", err)
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseDeleting,
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

func shouldReapplyDesiredSpec(item controlservice.Service) bool {
	if item.Status.DesiredState != controlservice.DesiredStateActive {
		return false
	}
	if item.Status.Observed.ObservedGeneration != item.Metadata.Generation {
		return false
	}
	switch item.Status.Observed.Phase {
	case controlservice.PhasePending, controlservice.PhaseDegraded:
		return true
	default:
		return false
	}
}
