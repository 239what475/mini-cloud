package servicecontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/controlplane/deploy"
	planeclient "mini-cloud/internal/controlplane/planeclient"
	"mini-cloud/internal/controlplane/planeselector"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/store"
)

var ErrPersistentDirsAutoMoveBlocked = errors.New("services with persistentDirs cannot be automatically moved to a different plane after a revision has been created")

func (c *Controller) reconcileService(ctx context.Context, item controlservice.Service) error {
	currentPlacement, hasPlacement, err := c.getPlacement(ctx, item.Metadata.ID)
	if err != nil {
		return err
	}

	switch {
	case item.Status.DesiredState == controlservice.DesiredStateDeleted:
		return c.reconcileServiceDeletion(ctx, item, hasPlacement, currentPlacement)
	case !hasPlacement:
		return c.reconcileServiceWithoutPlacement(ctx, item)
	case item.Metadata.Generation > item.Status.Observed.ObservedGeneration || shouldRetryDesiredSpec(item):
		return c.reconcileServiceDesiredSpec(ctx, item, currentPlacement)
	default:
		return nil
	}
}

func (c *Controller) reconcileServiceDeletion(ctx context.Context, serviceItem controlservice.Service, hasPlacement bool, currentPlacement controlservice.ServicePlacement) error {
	if !hasPlacement {
		if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
			!errors.Is(err, store.ErrServiceNotFound) &&
			!errors.Is(err, store.ErrServiceGenerationConflict) {
			return err
		}
		return nil
	}

	deleteErr := c.deploy.DeleteService(ctx, currentPlacement.PlaneID, serviceItem.Metadata.ID)
	if deleteErr != nil && !errors.Is(deleteErr, planeclient.ErrNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem.Metadata.Generation, deleteErr))
		return errors.Join(deleteErr, statusErr)
	}
	if err := c.store.DeleteServicePlacementForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
		!errors.Is(err, store.ErrServicePlacementNotFound) &&
		!errors.Is(err, store.ErrServiceGenerationConflict) {
		return err
	}
	if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
		!errors.Is(err, store.ErrServiceNotFound) &&
		!errors.Is(err, store.ErrServiceGenerationConflict) {
		return err
	}
	return nil
}

func (c *Controller) reconcileServiceWithoutPlacement(ctx context.Context, serviceItem controlservice.Service) error {
	if persistentDirsPlacementLocked(serviceItem) {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, false, false, ErrPersistentDirsAutoMoveBlocked))
	}
	decision, err := c.selectPlane(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, false, false, err))
		return errors.Join(err, statusErr)
	}
	if decision == nil {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, pendingPlaneSelectorStatus(serviceItem.Metadata.Generation, ErrNoEligiblePlacement))
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, *decision, nil)
	return err
}

func (c *Controller) reconcileServiceDesiredSpec(ctx context.Context, serviceItem controlservice.Service, currentPlacement controlservice.ServicePlacement) error {
	if c.canReusePlacement(ctx, serviceItem, currentPlacement) {
		_, err := c.applyServiceToCurrentPlacement(ctx, serviceItem, currentPlacement)
		return err
	}
	if serviceItem.Spec.PersistentDirsLocked {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, ErrPersistentDirsAutoMoveBlocked))
	}
	decision, err := c.selectPlane(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, err))
		return errors.Join(err, statusErr)
	}
	if decision == nil {
		return c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, pendingPlaneSelectorStatus(serviceItem.Metadata.Generation, ErrNoEligiblePlacement))
	}
	_, err = c.applyServiceToPlane(ctx, serviceItem, *decision, &currentPlacement)
	return err
}

func (c *Controller) applyServiceToCurrentPlacement(ctx context.Context, serviceItem controlservice.Service, currentPlacement controlservice.ServicePlacement) (*controlservice.ServicePlacement, error) {
	result, err := c.deploy.ApplyService(ctx, currentPlacement.PlaneID, toDeployApplyInput(serviceItem))
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, err))
		return nil, errors.Join(err, statusErr)
	}

	nextPlacement := acceptedPlacementFromApplyResult(serviceItem.Metadata.ID, result)
	updatedPlacement, err := c.store.UpsertServicePlacementForGeneration(ctx, nextPlacement, serviceItem.Metadata.Generation, len(serviceItem.Spec.PersistentDirs) > 0 || serviceItem.Spec.PersistentDirsLocked)
	if err != nil {
		if errors.Is(err, store.ErrServiceGenerationConflict) {
			return nil, nil
		}
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, err))
		return nil, errors.Join(err, statusErr)
	}
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, updatedPlacement, result))
	return &updatedPlacement, statusErr
}

func (c *Controller) applyServiceToPlane(ctx context.Context, serviceItem controlservice.Service, decision planeselector.Decision, previous *controlservice.ServicePlacement) (*controlservice.ServicePlacement, error) {
	result, err := c.deploy.ApplyService(ctx, decision.PlaneID, toDeployApplyInput(serviceItem))
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, decision.PlaneID != "", false, err))
		return nil, errors.Join(err, statusErr)
	}

	nextPlacement := acceptedPlacementFromApplyResult(serviceItem.Metadata.ID, result)
	if previous != nil && shouldMovePlacement(*previous, nextPlacement) {
		if err := c.deploy.DeleteService(ctx, previous.PlaneID, serviceItem.Metadata.ID); err != nil && !errors.Is(err, planeclient.ErrNotFound) {
			_ = c.deploy.DeleteService(ctx, nextPlacement.PlaneID, serviceItem.Metadata.ID)
			statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, fmt.Errorf("delete old remote service after move: %w", err)))
			return nil, errors.Join(err, statusErr)
		}
	}

	updatedPlacement, err := c.store.UpsertServicePlacementForGeneration(ctx, nextPlacement, serviceItem.Metadata.Generation, len(serviceItem.Spec.PersistentDirs) > 0 || serviceItem.Spec.PersistentDirsLocked)
	if err != nil {
		if errors.Is(err, store.ErrServiceGenerationConflict) {
			if previous == nil || shouldMovePlacement(*previous, nextPlacement) {
				_ = c.deploy.DeleteService(ctx, nextPlacement.PlaneID, serviceItem.Metadata.ID)
			}
			return nil, nil
		}
		if previous == nil || shouldMovePlacement(*previous, nextPlacement) {
			_ = c.deploy.DeleteService(ctx, nextPlacement.PlaneID, serviceItem.Metadata.ID)
		}
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, failedServiceStatus(serviceItem.Metadata.Generation, true, false, err))
		return nil, errors.Join(err, statusErr)
	}
	statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, executionPlanDispatchedStatus(serviceItem.Metadata.Generation, updatedPlacement, result))
	return &updatedPlacement, statusErr
}

func (c *Controller) selectPlane(ctx context.Context, serviceItem controlservice.Service) (*planeselector.Decision, error) {
	result, err := c.selector.PreviewSelection(ctx, planeselector.SelectionInput{
		Provider:      serviceItem.Spec.Provider,
		Region:        serviceItem.Spec.Region,
		PinnedPlaneID: serviceItem.Spec.PinnedPlaneID,
		InstanceClass: serviceItem.Spec.InstanceClass,
		Replicas:      serviceItem.Spec.Replicas,
	})
	if err != nil {
		return nil, err
	}
	return result.Decision, nil
}

func (c *Controller) canReusePlacement(ctx context.Context, serviceItem controlservice.Service, currentPlacement controlservice.ServicePlacement) bool {
	if strings.TrimSpace(currentPlacement.PlaneID) == "" {
		return false
	}
	plane, err := c.store.GetPlane(ctx, currentPlacement.PlaneID)
	if err != nil {
		return false
	}
	if strings.TrimSpace(serviceItem.Spec.PinnedPlaneID) != "" && plane.ID != serviceItem.Spec.PinnedPlaneID {
		return false
	}
	if plane.Provider != serviceItem.Spec.Provider || plane.Region != serviceItem.Spec.Region {
		return false
	}
	if !plane.Registration.Registered || !plane.Operation.AcceptingNewDeployments() {
		return false
	}
	return true
}

func (c *Controller) getPlacement(ctx context.Context, serviceID string) (controlservice.ServicePlacement, bool, error) {
	item, err := c.store.GetServicePlacement(ctx, serviceID)
	switch {
	case err == nil:
		return item, true, nil
	case errors.Is(err, store.ErrServicePlacementNotFound):
		return controlservice.ServicePlacement{}, false, nil
	default:
		return controlservice.ServicePlacement{}, false, err
	}
}

func acceptedPlacementFromApplyResult(serviceID string, result deploy.ApplyResult) controlservice.ServicePlacement {
	message := "execution plan accepted by cloud-plane; waiting for node-agent execution"
	if strings.TrimSpace(result.PlanID) != "" {
		message = fmt.Sprintf("execution plan %s accepted by cloud-plane; waiting for node-agent execution", result.PlanID)
	}
	return controlservice.ServicePlacement{
		ServiceID:     serviceID,
		PlaneID:       result.PlaneID,
		RemoteStatus:  "accepted",
		RemoteHealthy: false,
		RemoteMessage: message,
	}
}

func persistentDirsPlacementLocked(serviceItem controlservice.Service) bool {
	return serviceItem.Spec.PersistentDirsLocked
}

func shouldMovePlacement(current controlservice.ServicePlacement, next controlservice.ServicePlacement) bool {
	return current.PlaneID != next.PlaneID
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
			Replicas:             serviceItem.Spec.Replicas,
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
			PersistentDirs:       persistentdir.CloneSpecs(serviceItem.Spec.PersistentDirs),
		},
	}
}

func (c *Controller) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status controlservice.Status, rollout ...*controlservice.RolloutStatus) error {
	var nextRollout *controlservice.RolloutStatus
	if len(rollout) > 0 {
		nextRollout = rollout[0]
	}
	_, err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, controlservice.UpdateStatusInput{
		ObservedGeneration: status.ObservedGeneration,
		Phase:              status.Phase,
		Healthy:            status.Healthy,
		Message:            status.Message,
		Conditions:         controlservice.CloneConditions(status.Conditions),
		LastReconciledAt:   status.LastReconciledAt,
		Rollout:            nextRollout,
	})
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func executionPlanDispatchedStatus(generation int64, placementItem controlservice.ServicePlacement, result deploy.ApplyResult) controlservice.Status {
	now := time.Now().UTC()
	message := "execution plan dispatched; waiting for node-agent execution result"
	if strings.TrimSpace(result.PlanID) != "" {
		message = fmt.Sprintf("execution plan %s dispatched; waiting for node-agent execution result", result.PlanID)
	}
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseProgressing,
		Healthy:            false,
		Message:            message,
		Conditions: []controlservice.Condition{
			controlservice.NewCondition(controlservice.ConditionPlacementReady, controlservice.ConditionTrue, controlservice.ReasonApplied, fmt.Sprintf("service placed on plane %s", placementItem.PlaneID), generation, now),
			controlservice.NewCondition(controlservice.ConditionApplied, controlservice.ConditionTrue, controlservice.ReasonApplied, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionReady, controlservice.ConditionFalse, controlservice.ReasonPlaneServiceNotHealthy, "waiting for node-agent execution result", generation, now),
		},
		LastReconciledAt: &now,
	}
}

func pendingPlaneSelectorStatus(generation int64, err error) controlservice.Status {
	now := time.Now().UTC()
	message := "service is waiting for an eligible plane"
	reason := controlservice.ReasonNoPlacement
	if err != nil {
		message = err.Error()
		reason = controlservice.ReasonNoEligiblePlacement
	}
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhasePending,
		Healthy:            false,
		Message:            message,
		Conditions: []controlservice.Condition{
			controlservice.NewCondition(controlservice.ConditionPlacementReady, controlservice.ConditionFalse, reason, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionApplied, controlservice.ConditionFalse, reason, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionReady, controlservice.ConditionFalse, reason, message, generation, now),
		},
		LastReconciledAt: &now,
	}
}

func failedServiceStatus(generation int64, placementReady bool, applied bool, err error) controlservice.Status {
	now := time.Now().UTC()
	message := fmt.Sprintf("service reconcile failed: %v", err)
	placementCondition := controlservice.ConditionFalse
	if placementReady {
		placementCondition = controlservice.ConditionTrue
	}
	appliedCondition := controlservice.ConditionFalse
	if applied {
		appliedCondition = controlservice.ConditionTrue
	}
	return controlservice.Status{
		ObservedGeneration: generation,
		Phase:              controlservice.PhaseDegraded,
		Healthy:            false,
		Message:            message,
		Conditions: []controlservice.Condition{
			controlservice.NewCondition(controlservice.ConditionPlacementReady, placementCondition, controlservice.ReasonReconcileFailed, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionApplied, appliedCondition, controlservice.ReasonApplyFailed, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionReady, controlservice.ConditionFalse, controlservice.ReasonPlaneServiceNotHealthy, message, generation, now),
		},
		LastReconciledAt: &now,
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
		Conditions: []controlservice.Condition{
			controlservice.NewCondition(controlservice.ConditionPlacementReady, controlservice.ConditionTrue, controlservice.ReasonApplied, "service selection is kept while remote refresh is retried", generation, now),
			controlservice.NewCondition(controlservice.ConditionApplied, controlservice.ConditionTrue, controlservice.ReasonApplied, "last applied remote service state is kept while remote refresh is retried", generation, now),
			controlservice.NewCondition(controlservice.ConditionReady, controlservice.ConditionFalse, controlservice.ReasonObservationFailed, message, generation, now),
		},
		LastReconciledAt: &now,
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
		Conditions: []controlservice.Condition{
			controlservice.NewCondition(controlservice.ConditionPlacementReady, controlservice.ConditionFalse, controlservice.ReasonDeletionRequested, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionApplied, controlservice.ConditionFalse, controlservice.ReasonDeletionRequested, message, generation, now),
			controlservice.NewCondition(controlservice.ConditionReady, controlservice.ConditionFalse, controlservice.ReasonDeletionRequested, message, generation, now),
		},
		LastReconciledAt: &now,
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

func shouldRetryDesiredSpec(item controlservice.Service) bool {
	if item.Status.DesiredState != controlservice.DesiredStateActive {
		return false
	}
	for _, condition := range item.Status.Observed.Conditions {
		if condition.Type != controlservice.ConditionApplied {
			continue
		}
		if condition.ObservedGeneration != item.Metadata.Generation {
			continue
		}
		return condition.Status != controlservice.ConditionTrue
	}
	return false
}
