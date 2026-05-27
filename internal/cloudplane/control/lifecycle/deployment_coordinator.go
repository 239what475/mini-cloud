package lifecycle

import (
	"context"
	"fmt"
	"log/slog"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/control/nodepool"
	deploymentmodel "mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	infraruntimepool "mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
	"mini-cloud/internal/common/logctx"
)

type deploymentCoordinator struct {
	logger   *slog.Logger
	store    *store.Store
	config   cloudplaneconfig.Config
	scaleOut *nodepool.ScaleOutService
}

func newDeploymentCoordinator(logger *slog.Logger, stores *store.Store, cfg cloudplaneconfig.Config, driver infraruntimepool.RuntimeDriver) deploymentCoordinator {
	return deploymentCoordinator{
		logger:   logger,
		store:    stores,
		config:   cfg,
		scaleOut: nodepool.NewScaleOutService(logger, stores, cfg, driver),
	}
}

func (s deploymentCoordinator) launch(ctx context.Context, serviceItem workload.Service, revisionID string, creationReason string) (deploymentmodel.Deployment, workload.Service, *scheduler.StoredDecision, error) {
	createdDeployment, err := s.store.InsertDeployment(ctx, deploymentmodel.CreateInput{
		ServiceID:       serviceItem.Metadata.ID,
		RevisionID:      revisionID,
		DesiredReplicas: serviceItem.Spec.Replicas,
	}, creationReason)
	if err != nil {
		return deploymentmodel.Deployment{}, workload.Service{}, nil, err
	}
	return s.schedule(ctx, serviceItem, createdDeployment)
}

func (s deploymentCoordinator) scaleCurrentDeployment(ctx context.Context, serviceItem workload.Service) (deploymentmodel.Deployment, []scheduler.StoredDecision, error) {
	currentDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	if currentDeployment == nil {
		return deploymentmodel.Deployment{}, nil, store.ErrDeploymentNotFound
	}
	ctx = logctx.WithFields(ctx, logctx.Fields{
		ProjectID:    serviceItem.Metadata.ProjectID,
		ServiceID:    serviceItem.Metadata.ID,
		DeploymentID: currentDeployment.ID,
	})

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	cpuMilliRequest, memoryMiRequest, err := workload.ResourceRequest(serviceItem.Spec.InstanceClass)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	nodes, pinnedToActiveNode, err := s.pickCandidateNodes(ctx, serviceItem, nodes, cpuMilliRequest, memoryMiRequest)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	provider, err := s.resolvePlacementProvider()
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	request := scheduler.PlacementRequest{
		DeploymentID:    currentDeployment.ID,
		Provider:        provider,
		Region:          serviceItem.Spec.Region,
		CPUMilliRequest: cpuMilliRequest,
		MemoryMiRequest: memoryMiRequest,
		Replicas:        serviceItem.Spec.Replicas - currentDeployment.DesiredReplicas,
	}
	plan, err := scheduler.Plan(nodes, request)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	if len(plan.Decisions) == 0 {
		scaleOutResult, err := s.scaleOut.TryForPlacement(ctx, nodepool.ScaleOutRequest{
			Service:            serviceItem,
			Placement:          request,
			PinnedToActiveNode: pinnedToActiveNode,
			InitialFailure:     plan.FailureReason,
		})
		if err != nil {
			return deploymentmodel.Deployment{}, nil, err
		}
		if len(scaleOutResult.Nodes) > 0 {
			plan, err = scheduler.Plan(scaleOutResult.Nodes, request)
			if err != nil {
				return deploymentmodel.Deployment{}, nil, err
			}
		}
		if len(plan.Decisions) == 0 {
			failureReason := plan.FailureReason
			if failureReason == "" {
				failureReason = "no placement found for service scale-up"
			}
			if scaleOutResult.Failure != "" {
				failureReason = scaleOutResult.Failure
			} else if scaleOutResult.Summary != "" {
				failureReason = fmt.Sprintf("%s, but scheduling still failed: %s", scaleOutResult.Summary, failureReason)
			}
			return deploymentmodel.Deployment{}, nil, fmt.Errorf("%s", failureReason)
		}
	}
	for i := range plan.Decisions {
		plan.Decisions[i].DeploymentID = currentDeployment.ID
	}
	updated, placements, err := s.store.AddDeploymentPlacements(ctx, currentDeployment.ID, serviceItem.Spec.Replicas, "service replica count increased", request, plan.Decisions)
	if err != nil {
		return deploymentmodel.Deployment{}, nil, err
	}
	return updated, placements, nil
}
