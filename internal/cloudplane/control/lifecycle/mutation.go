package lifecycle

import (
	"context"
	"fmt"
	"strings"

	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// Create 创建 service，并立即启动初始 revision。
// 参数说明：ctx 控制数据库请求生命周期；serviceID/projectID/name 是 service 身份；spec 是 service 目标运行规格。
func (o ServiceMutationController) Create(ctx context.Context, serviceID string, projectID string, name string, displayName string, spec workload.Spec) (CreateResult, error) {
	// 先确认 project 存在，避免创建孤立 service。
	if _, err := o.store.GetProject(ctx, projectID); err != nil {
		return CreateResult{}, err
	}

	// 按输入规格创建 service 记录；创建过程会做领域校验和默认值处理。
	created, err := o.store.InsertService(ctx, serviceID, projectID, name, displayName, spec)
	if err != nil {
		return CreateResult{}, err
	}

	// 初始 service 创建后立即生成 revision 快照并启动 deployment。
	launch, err := o.launchRevision(ctx, created, "service created; initial revision snapshot and deployment created")
	if err != nil {
		return CreateResult{}, err
	}
	// 重新读取运行态，确保返回视图反映 launch 后的 deployment/execution 关联。
	runtimeState, err := (ServiceQueryController(o)).loadRuntimeState(ctx, launch.Service)
	if err != nil {
		return CreateResult{}, err
	}

	// 返回创建后的运行态和初始 launch 结果。
	return CreateResult{
		Runtime: runtimeState,
		Launch:  launch,
	}, nil
}

// Delete 删除或清理指定的 cloud-plane service。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是要删除的本地 service ID。
func (o ServiceMutationController) Delete(ctx context.Context, serviceID string) error {
	if strings.TrimSpace(serviceID) == "" {
		return ErrServiceIDRequired
	}
	if err := o.store.DeleteService(ctx, strings.TrimSpace(serviceID)); err != nil {
		return err
	}
	return nil
}

// Update 更新 service 规格，并在规格变化时启动新 revision 或扩容当前 deployment。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是当前 service 记录；spec 是目标 service 运行规格。
func (o ServiceMutationController) Update(ctx context.Context, serviceItem workload.Service, displayName string, spec workload.Spec) (UpdateResult, error) {
	// 复杂流程说明：更新先判断 spec 是否触发新 revision，再区分 revision rollout 与副本扩容。
	// 如果后续 launch 或 scale 失败，会尽量恢复旧 service spec；已产生的 revision/deployment 记录不在这里回滚。
	// 重新读取最新 service，避免调用方传入的 serviceItem 不是当前库中状态。
	latest, err := o.store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return UpdateResult{}, err
	}

	// exposure 先归一化，再执行 Spec 自身校验。
	spec.Exposure = spec.NormalizedExposure()
	if err := spec.Validate(); err != nil {
		return UpdateResult{}, err
	}

	// 在不落库的情况下预览目标 service，提前判断副本数变化。
	preview := servicePreviewForUpdate(latest, spec)
	replicasChanged := latest.Spec.Replicas != preview.Spec.Replicas
	if replicasChanged {
		// 当前 rollout 已有候选 revision 时，不允许同时改变 replicas。
		if latest.Status.CandidateRevisionID != "" {
			return UpdateResult{}, ErrScaleDuringRollout
		}
		// 当前系统没有 service replica 缩容实现，目标副本数小于当前副本数时直接拒绝。
		if preview.Spec.Replicas < latest.Spec.Replicas {
			return UpdateResult{}, ErrScaleDownNotSupported
		}
	}

	// 先写入 service 规格，store 会返回本次变更对 revision/replicas 的影响。
	updated, impact, err := o.store.UpdateServiceSpec(ctx, serviceItem.Metadata.ID, displayName, spec)
	if err != nil {
		return UpdateResult{}, err
	}

	// launch 仅在 revision 规格变化时产生；纯扩容不会创建新 revision。
	var launch *LaunchResult
	if impact.RevisionChanged {
		// revision 相关字段变化时，从更新后的 service 创建候选 revision 并启动 deployment。
		currentLaunch, err := o.launchRevision(ctx, updated, "service updated; new revision snapshot and deployment created")
		if err != nil {
			// launch 失败时尝试恢复旧 service spec，避免库中 spec 已变但没有对应 deployment。
			if restoreErr := o.restoreServiceSpec(ctx, latest); restoreErr != nil {
				return UpdateResult{}, fmt.Errorf("%w; restore previous service spec: %v", err, restoreErr)
			}
			return UpdateResult{}, err
		}
		updated = currentLaunch.Service
		launch = &currentLaunch
	} else if impact.ReplicasChanged {
		// 只有 replicas 变化时，在当前稳定 deployment 上追加 placement。
		if _, _, err := o.deploymentService.scaleCurrentDeployment(ctx, updated); err != nil {
			// 扩容失败同样尝试恢复旧 spec，使 desired replicas 回到扩容前。
			if restoreErr := o.restoreServiceSpec(ctx, latest); restoreErr != nil {
				return UpdateResult{}, fmt.Errorf("%w; restore previous service spec: %v", err, restoreErr)
			}
			return UpdateResult{}, err
		}
		// 已有 current revision 时，根据扩容方向更新 service 运行态。
		if updated.Status.CurrentRevisionID != "" {
			nextStatus := workload.StatusRunning
			// 新增副本刚写入 selection 时不一定已经 running，因此先标记 degraded。
			if impact.UpdatedReplicas > impact.PreviousReplicas {
				nextStatus = workload.StatusDegraded
			}
			// 纯扩容不产生候选 revision，rollout phase 保持 idle。
			updated, err = o.store.UpdateServiceRevisionState(
				ctx,
				updated.Metadata.ID,
				updated.Status.CurrentRevisionID,
				"",
				nextStatus,
				workload.RolloutPhaseIdle,
				"",
			)
			if err != nil {
				return UpdateResult{}, err
			}
		}
	}

	// 最终重新加载运行态，返回更新后 API view。
	runtimeState, err := (ServiceQueryController(o)).loadRuntimeState(ctx, updated)
	if err != nil {
		return UpdateResult{}, err
	}

	// RolloutTriggered 只表示本次是否创建了新 revision，不包含纯扩容。
	return UpdateResult{
		Runtime:          runtimeState,
		Service:          updated,
		RolloutTriggered: impact.RevisionChanged,
		Launch:           launch,
	}, nil
}

// launchRevision 从当前 service 规格创建 revision 快照，并启动对应 deployment。
// 参数说明：ctx 控制数据库请求生命周期；serviceItem 是要启动 revision 的 service；reason 记录本次发布原因。
func (o ServiceMutationController) launchRevision(ctx context.Context, serviceItem workload.Service, reason string) (LaunchResult, error) {
	// revision 是 service 当前规格的不可变快照，deployment 绑定 revision 而不是直接绑定可变 service spec。
	createdRevision, err := o.store.InsertRevisionFromService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		return LaunchResult{}, err
	}

	// 交给 deploymentService 走本地调度和必要的 runtime scale-out。
	currentDeployment, currentService, currentPlacement, err := o.deploymentService.launch(ctx, serviceItem, createdRevision.ID, reason)
	if err != nil {
		return LaunchResult{}, err
	}
	// 将 revision、deployment、更新后的 service 和代表性 selection 组合为 launch 结果。
	return LaunchResult{
		Revision:   createdRevision,
		Deployment: currentDeployment,
		Service:    currentService,
		Placement:  currentPlacement,
	}, nil
}

// restoreServiceSpec 在 launch/scale 失败后尝试把 service spec 恢复到给定旧记录。
// 参数说明：ctx 控制数据库请求生命周期；current 是需要写回的 service 规格。
func (o ServiceMutationController) restoreServiceSpec(ctx context.Context, current workload.Service) error {
	// 将当前 service 记录提取成 Spec，复用 store.UpdateServiceSpec 的更新路径恢复旧规格。
	_, _, err := o.store.UpdateServiceSpec(ctx, current.Metadata.ID, current.Metadata.DisplayName, workload.SpecFromService(current))
	return err
}

// servicePreviewForUpdate 在不落库的情况下计算更新后的 service 规格。
// 参数说明：current 是更新前的 service 记录；spec 是待应用的目标运行规格。
func servicePreviewForUpdate(current workload.Service, spec workload.Spec) workload.Service {
	// 从当前 service 复制一份预览对象，避免修改调用方持有的 current。
	preview := current
	// 以下字段按 Spec 覆盖，模拟 store.UpdateServiceSpec 成功后的目标规格。
	preview.Spec.Region = spec.Region
	preview.Spec.Replicas = spec.Replicas
	preview.Spec.InstanceClass = spec.InstanceClass
	// exposure 使用归一化结果，保证预览与真实更新路径一致。
	preview.Spec.Exposure = spec.Exposure
	preview.Spec.Image = spec.Image
	// 切片字段深拷贝，避免预览对象与输入共享底层数组。
	preview.Spec.Command = append([]string(nil), spec.Command...)
	preview.Spec.Args = append([]string(nil), spec.Args...)
	preview.Spec.DefaultPort = spec.DefaultPort
	preview.Spec.ReadinessPath = spec.ReadinessPath
	// map 字段深拷贝，避免后续修改输入影响预览。
	preview.Spec.Env = copyStringMap(spec.Env)
	preview.Spec.ConfigSetID = spec.ConfigSetID
	preview.Spec.SecretSetID = spec.SecretSetID
	preview.Spec.RegistryCredentialID = spec.RegistryCredentialID
	// projected files / persistent dirs 通过公共 clone helper 复制并规范化顺序。
	preview.Spec.ProjectedFiles = projectedfile.CloneSpecs(spec.ProjectedFiles)
	preview.Spec.PersistentDirs = persistentdir.CloneSpecs(spec.PersistentDirs)
	return preview
}
