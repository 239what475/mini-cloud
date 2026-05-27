package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/cloudplane/infra/store"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetService 查询 cloud-plane 本地已接受 service 及其当前观测状态。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带鉴权 metadata；req 表示请求参数。
func (s *Server) GetService(ctx context.Context, req *cloudplanev1.GetServiceRequest) (*cloudplanev1.GetServiceResponse, error) {
	// 查询 service 状态也属于内部 southbound API，必须先鉴权。
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(req.GetProjectId())
	serviceID := strings.TrimSpace(req.GetServiceId())
	if projectID == "" || serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID and serviceID are required")
	}

	serviceItem, err := s.store.GetService(ctx, serviceID)
	if err != nil {
		return nil, s.getStatusError("get accepted service", serviceID, err)
	}
	if serviceItem.Metadata.ProjectID != projectID {
		return nil, status.Error(codes.NotFound, store.ErrServiceNotFound.Error())
	}

	return &cloudplanev1.GetServiceResponse{
		Service: protoService(serviceFromDomain(serviceItem)),
		Status:  protoObservedServiceStatus(s.observedServiceStatus(ctx, serviceItem)),
	}, nil
}

// serviceFromDomain 将 cloud-plane 本地 service 领域模型转换为 southbound 响应模型。
// 参数说明：serviceItem 是 store 读取到的 service 持久化记录。
func serviceFromDomain(serviceItem workload.Service) cloudplaneapi.Service {
	return cloudplaneapi.Service{
		Metadata: cloudplaneapi.ServiceMetadata{
			ID:          serviceItem.Metadata.ID,
			ProjectID:   serviceItem.Metadata.ProjectID,
			Name:        serviceItem.Metadata.Name,
			DisplayName: serviceItem.Metadata.DisplayName,
		},
		Spec: cloudplaneapi.ServiceSpec{
			Region:               serviceItem.Spec.Region,
			Replicas:             serviceItem.Spec.Replicas,
			InstanceClass:        serviceItem.Spec.InstanceClass,
			Exposure:             workload.NormalizeExposure(serviceItem.Spec.Exposure),
			Image:                serviceItem.Spec.Image,
			Command:              append([]string(nil), serviceItem.Spec.Command...),
			Args:                 append([]string(nil), serviceItem.Spec.Args...),
			DefaultPort:          serviceItem.Spec.DefaultPort,
			ReadinessPath:        serviceItem.Spec.ReadinessPath,
			Env:                  controlplane.CopyStringMap(serviceItem.Spec.Env),
			ConfigSetID:          serviceItem.Spec.ConfigSetID,
			SecretSetID:          serviceItem.Spec.SecretSetID,
			RegistryCredentialID: serviceItem.Spec.RegistryCredentialID,
			ProjectedFiles:       serviceItem.Spec.ProjectedFiles,
			PersistentDirs:       serviceItem.Spec.PersistentDirs,
		},
		Status: cloudplaneapi.ServiceStatus{
			Phase:               serviceItem.Status.Phase,
			CurrentRevisionID:   serviceItem.Status.CurrentRevisionID,
			CandidateRevisionID: serviceItem.Status.CandidateRevisionID,
			RolloutPhase:        serviceItem.Status.RolloutPhase,
			RolloutMessage:      serviceItem.Status.RolloutMessage,
		},
	}
}

// observedServiceStatus 从 service 持久化状态和 deployment/execution 运行态构建观测状态。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是 store 读取到的 service 持久化记录。
func (s *Server) observedServiceStatus(ctx context.Context, serviceItem workload.Service) cloudplaneapi.ObservedServiceStatus {
	healthy, message := s.serviceHealth(ctx, serviceItem)
	return cloudplaneapi.ObservedServiceStatus{
		CurrentRevisionID: serviceItem.Status.CurrentRevisionID,
		Healthy:           healthy,
		Message:           message,
		Rollout:           s.observedRollout(ctx, serviceItem),
	}
}

// serviceHealth 根据 service、deployment 和 execution 状态生成健康布尔值和可读说明。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是 store 读取到的 service 持久化记录。
func (s *Server) serviceHealth(ctx context.Context, serviceItem workload.Service) (bool, string) {
	currentDeployment, err := s.currentObservedDeployment(ctx, serviceItem)
	if err != nil || currentDeployment == nil {
		if serviceItem.Status.RolloutPhase == workload.RolloutPhaseFailed && serviceItem.Status.RolloutMessage != "" {
			return false, serviceItem.Status.RolloutMessage
		}
		if serviceItem.Status.Phase == workload.StatusIdle {
			return false, "service exists but no deployment has been created yet"
		}
		if serviceItem.Status.CandidateRevisionID != "" {
			return false, "candidate revision is being deployed"
		}
		return false, "no deployment is currently recorded for this service"
	}

	currentExecution, err := s.store.GetLatestExecutionByDeployment(ctx, currentDeployment.ID)
	if err != nil {
		return false, "load current execution failed"
	}
	if currentExecution == nil || currentExecution.Status != execution.StatusRunning {
		runningExecutions, err := s.store.ListRunningExecutionsByDeployment(ctx, currentDeployment.ID)
		if err != nil {
			return false, "load running executions failed"
		}
		if len(runningExecutions) > 0 {
			currentExecution = &runningExecutions[0]
		}
	}

	if currentExecution != nil && currentExecution.Status == execution.StatusRunning &&
		currentDeployment.Status == deployment.StatusRunning &&
		serviceItem.Status.CurrentRevisionID != "" &&
		serviceItem.Status.CurrentRevisionID == currentDeployment.RevisionID {
		if serviceItem.Status.Phase == workload.StatusDegraded {
			return false, "current revision is serving traffic with fewer ready replicas than desired"
		}
		switch serviceItem.Status.RolloutPhase {
		case workload.RolloutPhaseProgressing:
			return true, "current revision is serving traffic while the candidate revision is progressing"
		case workload.RolloutPhaseFailed:
			return false, fallbackString(serviceItem.Status.RolloutMessage, "candidate revision failed while the current revision is still serving traffic")
		default:
			return true, "current revision is running"
		}
	}

	if serviceItem.Status.CurrentRevisionID != "" && currentDeployment.RevisionID != serviceItem.Status.CurrentRevisionID {
		switch serviceItem.Status.RolloutPhase {
		case workload.RolloutPhaseFailed:
			return false, fallbackString(serviceItem.Status.RolloutMessage, "candidate revision failed")
		default:
			return false, "a candidate revision rollout is still in progress"
		}
	}

	return false, fmt.Sprintf("service=%s deployment=%s", serviceItem.Status.Phase, currentDeployment.Status)
}

// currentObservedDeployment 返回当前应展示的 deployment；有稳定 revision 时优先返回 promoted deployment。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是 store 读取到的 service 持久化记录。
func (s *Server) currentObservedDeployment(ctx context.Context, serviceItem workload.Service) (*deployment.Deployment, error) {
	if serviceItem.Status.CurrentRevisionID != "" {
		return s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID)
	}
	return s.store.GetCurrentDeploymentByService(ctx, serviceItem.Metadata.ID)
}

// fallbackString 在首选值为空时返回备用值。
// 参数说明：preferred 是优先使用的字符串；fallback 是 preferred 为空时的备用值。
func fallbackString(preferred string, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

// observedRollout 构建 rollout 观测状态，并补齐 stable/candidate deployment 副本计数。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是 service 持久化记录。
func (s *Server) observedRollout(ctx context.Context, serviceItem workload.Service) cloudplaneapi.ObservedRolloutStatus {
	out := cloudplaneapi.ObservedRolloutStatus{
		Phase:               workload.NormalizeRolloutPhase(serviceItem.Status.RolloutPhase),
		Message:             serviceItem.Status.RolloutMessage,
		StableRevisionID:    serviceItem.Status.CurrentRevisionID,
		CandidateRevisionID: serviceItem.Status.CandidateRevisionID,
		ObservedAt:          time.Now().UTC(),
	}
	if stableDeployment, err := s.store.GetPromotedDeploymentByService(ctx, serviceItem.Metadata.ID); err == nil && stableDeployment != nil {
		out.StableDesiredReplicas = stableDeployment.DesiredReplicas
		out.StableReadyReplicas = stableDeployment.ReadyReplicas
		out.StableAvailableReplicas = stableDeployment.AvailableReplicas
		out.ObservedAt = stableDeployment.UpdatedAt.UTC()
	}
	if currentDeployment, err := s.store.GetCurrentDeploymentByService(ctx, serviceItem.Metadata.ID); err == nil && currentDeployment != nil {
		if serviceItem.Status.CandidateRevisionID != "" && currentDeployment.RevisionID == serviceItem.Status.CandidateRevisionID {
			out.CandidateDesiredReplicas = currentDeployment.DesiredReplicas
			out.CandidateReadyReplicas = currentDeployment.ReadyReplicas
			out.CandidateAvailableReplicas = currentDeployment.AvailableReplicas
			out.ObservedAt = currentDeployment.UpdatedAt.UTC()
		} else if serviceItem.Status.CurrentRevisionID == "" && currentDeployment.RevisionID != "" {
			out.CandidateRevisionID = currentDeployment.RevisionID
			out.CandidateDesiredReplicas = currentDeployment.DesiredReplicas
			out.CandidateReadyReplicas = currentDeployment.ReadyReplicas
			out.CandidateAvailableReplicas = currentDeployment.AvailableReplicas
			out.ObservedAt = currentDeployment.UpdatedAt.UTC()
		}
	}
	return out
}
