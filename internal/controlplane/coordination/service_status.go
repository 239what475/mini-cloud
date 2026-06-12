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

func dispatchedRunStatus() model.RunStatus {
	return model.RunStatus{
		Phase:   model.RunPhaseDispatching,
		Message: "service accepted by cloud-plane; waiting for node-agent execution result",
	}
}

func deletingRunStatus() model.RunStatus {
	return model.RunStatus{
		Phase:   model.RunPhaseDispatching,
		Message: "service delete accepted by cloud-plane; waiting for node-agent cleanup result",
	}
}

func (c *ServiceOperations) targetPlaneID(ctx context.Context, serviceItem model.Service) (string, error) {
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

func (c *ServiceOperations) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status model.ServiceObservedStatus, run *model.RunStatus) error {
	input := store.UpdateServiceStatusInput{
		ObservedGeneration: status.ObservedGeneration,
		Phase:              status.Phase,
		Message:            status.Message,
		LastObservedAt:     status.LastObservedAt,
		Run:                run,
	}
	err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, input)
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func serviceAcceptedStatus(generation int64) model.ServiceObservedStatus {
	now := time.Now().UTC()
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseProgressing,
		Message:            "service accepted by cloud-plane",
		LastObservedAt:     &now,
	}
}

func serviceDeletingStatus(generation int64) model.ServiceObservedStatus {
	now := time.Now().UTC()
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseDeleting,
		Message:            "service delete accepted by cloud-plane",
		LastObservedAt:     &now,
	}
}

func failedServiceStatus(serviceItem model.Service, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service dispatch failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		Phase:              model.PhaseDegraded,
		Message:            message,
		LastObservedAt:     &now,
	}
}

func deletingFailureServiceStatus(serviceItem model.Service, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service teardown failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		Phase:              model.PhaseDeleting,
		Message:            message,
		LastObservedAt:     &now,
	}
}
