package operatorapi

import (
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/servicecontroller"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamppb.New(value.UTC())
}

func requiredTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value.UTC())
}

func protoOverview(item overviewCounts) *controlplanev1.Overview {
	return &controlplanev1.Overview{
		PlanesTotal:         int32(item.PlanesTotal),
		PlanesRegistering:   int32(item.PlanesRegistering),
		PlanesReady:         int32(item.PlanesReady),
		PlanesDegraded:      int32(item.PlanesDegraded),
		PlanesOffline:       int32(item.PlanesOffline),
		ServicesTotal:       int32(item.ServicesTotal),
		ServicesPending:     int32(item.ServicesPending),
		ServicesProgressing: int32(item.ServicesProgressing),
		ServicesReady:       int32(item.ServicesReady),
		ServicesDegraded:    int32(item.ServicesDegraded),
		ServicesDeleting:    int32(item.ServicesDeleting),
	}
}

func protoPlane(item plane.Detail) *controlplanev1.Plane {
	out := &controlplanev1.Plane{
		Id:           item.ID,
		Name:         item.Name,
		DisplayName:  item.DisplayName,
		Provider:     item.Provider,
		Region:       item.Region,
		GrpcEndpoint: item.GRPCEndpoint,
		CreatedAt:    requiredTimestamp(item.CreatedAt),
		Status: &controlplanev1.PlaneStatus{
			Status:               string(item.Status.Status),
			Message:              item.Status.Message,
			LastHeartbeatAt:      optionalTimestamp(item.Status.LastHeartbeatAt),
			LastSyncAt:           optionalTimestamp(item.Status.LastSyncAt),
			LastInventoryVersion: item.Status.LastInventoryVersion,
			UpdatedAt:            requiredTimestamp(item.Status.UpdatedAt),
		},
		Registration: &controlplanev1.PlaneRegistration{
			Registered:     item.Registration.Registered,
			LastVerifiedAt: optionalTimestamp(item.Registration.LastVerifiedAt),
			TokenUpdatedAt: optionalTimestamp(item.Registration.TokenUpdatedAt),
		},
		Operation: &controlplanev1.PlaneOperation{
			State:     item.Operation.ResolvedState(),
			Reason:    item.Operation.Reason,
			UpdatedAt: requiredTimestamp(item.Operation.UpdatedAt),
		},
	}
	if item.LatestCapacityRecord != nil {
		out.LatestCapacitySnapshot = &controlplanev1.PlaneCapacitySnapshot{
			Id:                item.LatestCapacityRecord.ID,
			NodesTotal:        int32(item.LatestCapacityRecord.NodesTotal),
			NodesReady:        int32(item.LatestCapacityRecord.NodesReady),
			ServicesTotal:     int32(item.LatestCapacityRecord.ServicesTotal),
			RunsTotal:         int32(item.LatestCapacityRecord.RunsTotal),
			CpuMilliCapacity:  int32(item.LatestCapacityRecord.CPUMilliCapacity),
			CpuMilliAllocated: int32(item.LatestCapacityRecord.CPUMilliAllocated),
			MemoryMiCapacity:  int32(item.LatestCapacityRecord.MemoryMiCapacity),
			MemoryMiAllocated: int32(item.LatestCapacityRecord.MemoryMiAllocated),
			CapturedAt:        requiredTimestamp(item.LatestCapacityRecord.CapturedAt),
		}
	}
	if item.LatestRuntimeInventory != nil {
		out.LatestRuntimeInventory = &controlplanev1.PlaneRuntimeInventory{
			SyncVersion:       item.LatestRuntimeInventory.SyncVersion,
			ObservedAt:        requiredTimestamp(item.LatestRuntimeInventory.ObservedAt),
			NodesTotal:        int32(item.LatestRuntimeInventory.NodesTotal),
			NodesReady:        int32(item.LatestRuntimeInventory.NodesReady),
			CpuMilliCapacity:  int32(item.LatestRuntimeInventory.CPUMilliCapacity),
			CpuMilliAllocated: int32(item.LatestRuntimeInventory.CPUMilliAllocated),
			MemoryMiCapacity:  int32(item.LatestRuntimeInventory.MemoryMiCapacity),
			MemoryMiAllocated: int32(item.LatestRuntimeInventory.MemoryMiAllocated),
			UpdatedAt:         requiredTimestamp(item.LatestRuntimeInventory.UpdatedAt),
		}
	}
	if item.LatestRuntimeConfig != nil {
		runtimeConfig := &controlplanev1.PlaneRuntimeConfig{
			ObservedAt:  requiredTimestamp(item.LatestRuntimeConfig.ObservedAt),
			Fingerprint: item.LatestRuntimeConfig.Fingerprint,
			UpdatedAt:   requiredTimestamp(item.LatestRuntimeConfig.UpdatedAt),
		}
		if len(item.LatestRuntimeConfig.Summary) > 0 {
			if summary, err := structpb.NewStruct(item.LatestRuntimeConfig.Summary); err == nil {
				runtimeConfig.Summary = summary
			}
		}
		out.LatestRuntimeConfig = runtimeConfig
	}
	return out
}

func protoService(view servicecontroller.View) *controlplanev1.Service {
	out := &controlplanev1.Service{
		Metadata: &controlplanev1.ServiceMetadata{
			Id:          view.Service.Metadata.ID,
			Name:        view.Service.Metadata.Name,
			DisplayName: view.Service.Metadata.DisplayName,
			Generation:  view.Service.Metadata.Generation,
		},
		Spec: &controlplanev1.ServiceSpec{
			Provider:             view.Service.Spec.Provider,
			Region:               view.Service.Spec.Region,
			PinnedPlaneId:        view.Service.Spec.PinnedPlaneID,
			InstanceClass:        view.Service.Spec.InstanceClass,
			Exposure:             view.Service.Spec.Exposure,
			Image:                view.Service.Spec.Image,
			Command:              append([]string(nil), view.Service.Spec.Command...),
			Args:                 append([]string(nil), view.Service.Spec.Args...),
			DefaultPort:          int32(view.Service.Spec.DefaultPort),
			ReadinessPath:        view.Service.Spec.ReadinessPath,
			Env:                  copyStringMap(view.Service.Spec.Env),
			ConfigSetId:          view.Service.Spec.ConfigSetID,
			SecretSetId:          view.Service.Spec.SecretSetID,
			RegistryCredentialId: view.Service.Spec.RegistryCredentialID,
			ProjectedFiles:       projectedFilesToProto(view.Service.Spec.ProjectedFiles),
			PersistentDirs:       persistentDirsToProto(view.Service.Spec.PersistentDirs),
		},
		Status: &controlplanev1.ServiceStatus{
			DesiredState:       string(view.Service.Status.DesiredState),
			ObservedGeneration: view.Service.Status.Observed.ObservedGeneration,
			Phase:              view.Service.Status.Observed.Phase,
			Healthy:            view.Service.Status.Observed.Healthy,
			Message:            view.Service.Status.Observed.Message,
			LastReconciledAt:   optionalTimestamp(view.Service.Status.Observed.LastReconciledAt),
			Run: &controlplanev1.ServiceRunStatus{
				CurrentRunId:   view.Service.Status.Run.CurrentRunID,
				LatestRunId:    view.Service.Status.Run.LatestRunID,
				Phase:          view.Service.Status.Run.Phase,
				Message:        view.Service.Status.Run.Message,
				LastObservedAt: optionalTimestamp(view.Service.Status.Run.LastObservedAt),
			},
		},
		CreatedAt: requiredTimestamp(view.Service.CreatedAt),
		UpdatedAt: requiredTimestamp(view.Service.UpdatedAt),
	}
	if view.Placement != nil {
		out.Status.Placement = &controlplanev1.ServicePlacement{
			PlaneId:       view.Placement.PlaneID,
			RemoteStatus:  view.Placement.RemoteStatus,
			RemoteHealthy: view.Placement.RemoteHealthy,
			RemoteMessage: view.Placement.RemoteMessage,
		}
	}
	return out
}

func projectedFilesToProto(items []projectedfile.Spec) []*controlplanev1.ProjectedFileSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]*controlplanev1.ProjectedFileSpec, 0, len(items))
	for _, item := range projectedfile.CloneSpecs(items) {
		out = append(out, &controlplanev1.ProjectedFileSpec{
			MountPath:  item.MountPath,
			SourceKind: string(item.SourceKind),
			SourceId:   item.SourceID,
			SourceKey:  item.SourceKey,
		})
	}
	return out
}

func persistentDirsToProto(items []persistentdir.Spec) []*controlplanev1.PersistentDirSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]*controlplanev1.PersistentDirSpec, 0, len(items))
	for _, item := range persistentdir.CloneSpecs(items) {
		out = append(out, &controlplanev1.PersistentDirSpec{
			Name:      item.Name,
			MountPath: item.MountPath,
		})
	}
	return out
}
