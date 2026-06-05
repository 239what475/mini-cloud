package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// ErrExecutionNotFound 定义当前 cloud-plane 模块复用的错误或状态变量。
var ErrExecutionNotFound = errors.New("execution not found")

// CreateExecutionClaim 为指定 node 原子领取一个尚未执行的 deployment。
// 参数说明：ctx 控制本次请求或后台操作生命周期；nodeID 是 node 唯一标识。
func (s *Store) createLegacyDeploymentExecutionClaim(ctx context.Context, nodeID string) (*execution.WorkItem, error) {
	// node-agent claim work 必须在事务内完成，避免多个节点领取同一 execution。
	// 阶段一：锁定最早可执行的 deployment，并排除已有 deploying/running execution。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim execution tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var work execution.WorkItem
	var currentStatus string
	var commandJSON []byte
	var argsJSON []byte
	var revisionEnvJSON []byte
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var secretValuesJSON []byte
	var registryServer sql.NullString
	var registryUsername sql.NullString
	var registryPassword sql.NullString
	var currentRevisionID sql.NullString
	var cpuMilliRequest int
	var memoryMiRequest int

	// 查询一条分配给当前 node、尚未有 deploying/running execution、
	// 且当前 node 仍然 ready/schedulable 的任务；draining 节点不能继续领取新 work。
	err = tx.QueryRowContext(ctx, `
			SELECT
				d.id,
				d.status,
				a.id,
				a.name,
			r.id,
			r.label,
			r.image,
			r.command_json,
			r.args_json,
			r.env_json,
			r.projected_files_json,
			r.persistent_dirs_json,
			COALESCE(ps.values_json, '{}'::jsonb),
			prc.server,
			prc.username,
			prc.password,
			a.current_revision_id,
			r.port,
			r.readiness_path,
			pd.node_id,
			pd.cpu_milli_request,
			pd.memory_mi_request
		FROM deployments d
		JOIN services a ON a.id = d.service_id
		JOIN revisions r ON r.id = d.revision_id
		LEFT JOIN secret_sets ps ON ps.id = r.secret_set_id
		LEFT JOIN registry_credentials prc ON prc.id = r.registry_credential_id
		JOIN LATERAL (
			SELECT
				node_id,
				cpu_milli_request,
				memory_mi_request
			FROM placement_decisions
			WHERE deployment_id = d.id
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		) pd ON TRUE
		JOIN nodes n ON n.id = pd.node_id
		LEFT JOIN deployment_executions existing
			ON existing.deployment_id = d.id
		   AND existing.status IN ($4, $5)
		WHERE d.status IN ($1, $2, $3)
		  AND pd.node_id = $6
		  AND n.status = $7
		  AND n.schedulable = TRUE
		  AND existing.id IS NULL
		ORDER BY d.created_at ASC, d.id ASC
		LIMIT 1
		FOR UPDATE OF d, n
		`, deployment.StatusAssigned, deployment.StatusDeploying, deployment.StatusRunning, execution.StatusDeploying, execution.StatusRunning, nodeID, node.StatusReady).Scan(
		&work.DeploymentID,
		&currentStatus,
		&work.ServiceID,
		&work.ServiceName,
		&work.RevisionID,
		&work.RevisionLabel,
		&work.Image,
		&commandJSON,
		&argsJSON,
		&revisionEnvJSON,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&secretValuesJSON,
		&registryServer,
		&registryUsername,
		&registryPassword,
		&currentRevisionID,
		&work.ContainerPort,
		&work.ReadinessPath,
		&work.NodeID,
		&cpuMilliRequest,
		&memoryMiRequest,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有可领取任务时返回 nil work，node-agent 可稍后再次轮询。
			return nil, nil
		}
		return nil, fmt.Errorf("load claimable execution work: %w", err)
	}

	// 阶段二：把 revision 快照中的 command/env/projected files/persistent dirs 物化成 work item。
	if err := unmarshalJSON(commandJSON, &work.Command, []string{}); err != nil {
		return nil, fmt.Errorf("decode work command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &work.Args, []string{}); err != nil {
		return nil, fmt.Errorf("decode work args: %w", err)
	}
	var revisionEnv map[string]string
	if err := unmarshalJSON(revisionEnvJSON, &revisionEnv, map[string]string{}); err != nil {
		return nil, fmt.Errorf("decode revision env: %w", err)
	}
	// secret set 以 env map 形式并入 revision env；key 冲突时 secret 值覆盖 revision env。
	var secretEnv map[string]string
	if err := unmarshalJSON(secretValuesJSON, &secretEnv, map[string]string{}); err != nil {
		return nil, fmt.Errorf("decode secret env: %w", err)
	}
	work.Env = mergeStringMaps(revisionEnv, secretEnv)
	// projected file 需要读取全局 config/secret 实际值后才能下发给 node-agent。
	var projectedSpecs []projectedfile.Spec
	if err := unmarshalJSON(projectedFilesJSON, &projectedSpecs, []projectedfile.Spec{}); err != nil {
		return nil, fmt.Errorf("decode projected files: %w", err)
	}
	projectedFiles, err := s.materializeProjectedFiles(ctx, projectedSpecs)
	if err != nil {
		return nil, err
	}
	work.ProjectedFiles = projectedFiles
	// persistent dir 只需要根据 service ID 和 spec 计算宿主机挂载信息。
	var persistentSpecs []persistentdir.Spec
	if err := unmarshalJSON(persistentDirsJSON, &persistentSpecs, []persistentdir.Spec{}); err != nil {
		return nil, fmt.Errorf("decode persistent dirs: %w", err)
	}
	persistentDirs, err := persistentdir.MaterializeMounts("", work.ServiceID, persistentSpecs)
	if err != nil {
		return nil, fmt.Errorf("materialize persistent dirs: %w", err)
	}
	work.PersistentDirs = persistentDirs
	// registry credential 可选；存在时随 work item 下发给 node-agent 拉取镜像。
	if registryServer.Valid {
		work.ImageCredential = &execution.ImageCredential{
			Server:   registryServer.String,
			Username: registryUsername.String,
			Password: registryPassword.String,
		}
	}
	// 如果 service 已经有 current revision，尝试找出同 node 上旧 revision 的 running execution。
	if currentRevisionID.Valid && currentRevisionID.String != "" && currentRevisionID.String != work.RevisionID {
		superseded, err := loadRunningExecutionForRevisionOnNode(ctx, tx, work.ServiceID, currentRevisionID.String, nodeID, "")
		if err != nil {
			return nil, err
		}
		if superseded != nil {
			work.SupersededExecution = superseded
		}
	}

	// 为本次领取创建 execution ID 和容器名。
	work.ExecutionID, err = newID("exe")
	if err != nil {
		return nil, err
	}
	work.ContainerName = fmt.Sprintf("mini-cloud-%s", work.DeploymentID)

	// 第一次领取 assigned deployment 时，把 deployment 推进到 deploying。
	reason := fmt.Sprintf("agent on node %s started runtime execution", nodeID)
	if currentStatus == deployment.StatusAssigned {
		// 状态迁移先走领域校验，避免 store 写出非法状态。
		if err := deployment.ValidateTransition(currentStatus, deployment.StatusDeploying, reason); err != nil {
			return nil, err
		}

		// 更新 deployment 当前状态。
		if _, err := tx.ExecContext(ctx, `
			UPDATE deployments
			SET
				status = $2,
				status_reason = $3,
				updated_at = now()
			WHERE id = $1
		`, work.DeploymentID, deployment.StatusDeploying, reason); err != nil {
			return nil, fmt.Errorf("update deployment to deploying: %w", err)
		}

		// 记录 deployment 状态迁移历史。
		fromStatus := currentStatus
		if err := insertDeploymentTransition(ctx, tx, work.DeploymentID, &fromStatus, deployment.StatusDeploying, reason); err != nil {
			return nil, err
		}
	}

	// 插入 execution 记录，表示该 deployment 已被当前 node 领取并进入 deploying。
	startedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deployment_executions (
			id,
			deployment_id,
			node_id,
			image,
			container_name,
			container_port,
			readiness_path,
			status,
			status_reason,
			started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		work.ExecutionID,
		work.DeploymentID,
		nodeID,
		work.Image,
		work.ContainerName,
		work.ContainerPort,
		work.ReadinessPath,
		execution.StatusDeploying,
		reason,
		startedAt,
	); err != nil {
		return nil, fmt.Errorf("insert deployment execution: %w", err)
	}

	// 领取成功后立即增加 node allocation，避免同一容量被后续调度重复使用。
	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocated = cpu_milli_allocated + $2,
			memory_mi_allocated = memory_mi_allocated + $3,
			updated_at = now()
		WHERE id = $1
	`, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
		return nil, fmt.Errorf("reserve node allocation for execution: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim execution tx: %w", err)
	}

	// 返回完整 work item，node-agent 后续按该快照启动容器。
	return &work, nil
}

// materializeProjectedFiles 把 projected file spec 按 config/secret 值解析成 node-agent 可下发文件。
// 参数说明：ctx 控制本次请求或后台操作生命周期；specs 是本次操作的输入结构。
func (s *Store) materializeProjectedFiles(ctx context.Context, specs []projectedfile.Spec) ([]projectedfile.File, error) {
	// 没有 projected file spec 时无需查询 config/secret。
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]projectedfile.File, 0, len(specs))
	// CloneSpecs 避免后续处理意外修改调用方传入的 spec 切片。
	for _, item := range projectedfile.CloneSpecs(specs) {
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			// config set 来源读取明文配置值。
			configSet, err := s.resolveConfigSet(ctx, item.SourceID)
			if err != nil {
				return nil, err
			}
			// source key 必须存在，否则无法生成目标文件内容。
			value, ok := configSet.Values[item.SourceKey]
			if !ok {
				return nil, fmt.Errorf("config set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
			// config 文件使用 config 默认权限，不标记敏感。
			out = append(out, projectedfile.File{
				MountPath: item.MountPath,
				Content:   value,
				Mode:      projectedfile.DefaultMode(projectedfile.SourceKindConfigSet),
			})
		case projectedfile.SourceKindSecretSet:
			// secret set 来源读取敏感配置值。
			secretSet, err := s.resolveSecretSet(ctx, item.SourceID)
			if err != nil {
				return nil, err
			}
			// source key 必须存在，否则无法生成目标文件内容。
			value, ok := secretSet.Values[item.SourceKey]
			if !ok {
				return nil, fmt.Errorf("secret set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
			// secret 文件使用 secret 默认权限，并标记 Sensitive 供下游避免误记录。
			out = append(out, projectedfile.File{
				MountPath: item.MountPath,
				Content:   value,
				Mode:      projectedfile.DefaultMode(projectedfile.SourceKindSecretSet),
				Sensitive: true,
			})
		default:
			// Validate 漏掉的未知来源在这里兜底拒绝。
			return nil, projectedfile.ErrSourceKindInvalid
		}
	}
	// 返回克隆结果，避免调用方复用时影响本函数组装出的中间切片。
	return projectedfile.CloneFiles(out), nil
}

// UpdateExecutionFromNodeReport 接收 node-agent 执行结果，并原子推进 execution、deployment、service 和节点占用状态。
// 参数说明：ctx 控制本次请求或后台操作生命周期；nodeID 是 node 唯一标识；executionID 表示 execution 的唯一标识；input 是本次操作的输入结构。
func (s *Store) updateLegacyDeploymentExecutionFromNodeReport(ctx context.Context, nodeID string, executionID string, input execution.ReportInput) (execution.ReportAck, deployment.Deployment, workload.Service, error) {
	// execution report 会同时影响 execution、deployment、service rollout 和 node allocation。
	// 阶段一：校验上报并锁定 execution、deployment，同时读取 selection 资源占用。
	if err := input.Validate(); err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("begin report execution tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var current execution.Record
	var currentDeployment deployment.Deployment
	var cpuMilliRequest int
	var memoryMiRequest int
	// 锁定 execution 和 deployment，并读取 selection 中记录的资源占用。
	err = tx.QueryRowContext(ctx, `
		SELECT
			e.id,
			e.deployment_id,
			e.node_id,
			e.image,
			e.container_name,
			e.container_id,
			e.container_port,
			e.host_port,
			e.readiness_path,
			e.status,
			e.status_reason,
			e.started_at,
			e.finished_at,
			e.created_at,
			e.updated_at,
			d.id,
			d.service_id,
			d.revision_id,
			d.status,
			d.status_reason,
			d.created_at,
			d.updated_at,
			pd.cpu_milli_request,
			pd.memory_mi_request
		FROM deployment_executions e
		JOIN deployments d ON d.id = e.deployment_id
		JOIN placement_decisions pd
			ON pd.deployment_id = d.id
		WHERE e.id = $1
		  AND e.node_id = $2
		FOR UPDATE OF e, d
	`, executionID, nodeID).Scan(
		&current.ID,
		&current.DeploymentID,
		&current.NodeID,
		&current.Image,
		&current.ContainerName,
		&current.ContainerID,
		&current.ContainerPort,
		&current.HostPort,
		&current.ReadinessPath,
		&current.Status,
		&current.StatusReason,
		&current.StartedAt,
		&current.FinishedAt,
		&current.CreatedAt,
		&current.UpdatedAt,
		&currentDeployment.ID,
		&currentDeployment.ServiceID,
		&currentDeployment.RevisionID,
		&currentDeployment.Status,
		&currentDeployment.StatusReason,
		&currentDeployment.CreatedAt,
		&currentDeployment.UpdatedAt,
		&cpuMilliRequest,
		&memoryMiRequest,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// execution 不存在或不属于该 node 时，返回明确 not found。
			return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, ErrExecutionNotFound
		}
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("load execution for report: %w", err)
	}

	// 阶段二：处理重复上报和非法状态迁移；只有 deploying execution 可以进入新终态。
	observedAt := time.Now().UTC()
	if current.Status != execution.StatusDeploying {
		// running/failed 的同状态重复上报视为幂等成功。
		if current.Status == input.Status && (current.Status == execution.StatusRunning || current.Status == execution.StatusFailed) {
			if err := tx.Commit(); err != nil {
				return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("commit idempotent execution report tx: %w", err)
			}
			return execution.ReportAck{
				Execution:  current,
				ObservedAt: observedAt,
			}, currentDeployment, workload.Service{}, nil
		}
		// 其他终态或反向迁移不允许覆盖。
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf(
			"execution %s is already %s and cannot transition to %s",
			executionID,
			current.Status,
			input.Status,
		)
	}

	// 锁定 service 的 current/candidate revision 状态，后续会根据 execution 结果推进 rollout。
	revisionState, err := loadLockedServiceRevisionState(ctx, tx, currentDeployment.ServiceID)
	if err != nil {
		if errors.Is(err, ErrServiceNotFound) {
			return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, ErrServiceNotFound
		}
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("load service revision state before execution report: %w", err)
	}

	// 复制当前 service revision 状态，后续按执行结果计算目标状态。
	nextCurrentRevisionID := revisionState.CurrentRevisionID
	nextCandidateRevisionID := revisionState.CandidateRevisionID
	nextRolloutPhase := revisionState.RolloutPhase
	nextRolloutMessage := revisionState.RolloutMessage

	// 默认认为失败会让 service failed；running 分支会覆盖这个默认值。
	targetServiceStatus := workload.StatusFailed
	switch input.Status {
	case execution.StatusRunning:
	case execution.StatusFailed:
	default:
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("unsupported execution report status %q", input.Status)
	}
	// deployment 默认保持原状态，后续根据副本 ready 情况调整。
	targetDeploymentStatus := currentDeployment.Status

	// 只有 failed execution 设置 finished_at；running execution 仍表示活跃实例。
	var finishedAt sql.NullTime
	if input.Status == execution.StatusFailed {
		finishedAt = sql.NullTime{Time: observedAt, Valid: true}
	}

	// 写入 node-agent 上报的容器身份、host port 和 execution 终态。
	var updated execution.Record
	err = tx.QueryRowContext(ctx, `
		UPDATE deployment_executions
		SET
			container_name = $2,
			container_id = $3,
			host_port = $4,
			status = $5,
			status_reason = $6,
			finished_at = $7,
			updated_at = now()
		WHERE id = $1
		RETURNING id, deployment_id, node_id, image, container_name, container_id, container_port, host_port, readiness_path, status, status_reason, started_at, finished_at, created_at, updated_at
	`,
		executionID,
		input.ContainerName,
		input.ContainerID,
		input.HostPort,
		input.Status,
		input.Reason,
		finishedAt,
	).Scan(
		&updated.ID,
		&updated.DeploymentID,
		&updated.NodeID,
		&updated.Image,
		&updated.ContainerName,
		&updated.ContainerID,
		&updated.ContainerPort,
		&updated.HostPort,
		&updated.ReadinessPath,
		&updated.Status,
		&updated.StatusReason,
		&updated.StartedAt,
		&updated.FinishedAt,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("update execution report: %w", err)
	}

	// candidate revision running 后由 legacy cloud-plane lifecycle 在本地事务中自动转正。
	// 这段逻辑仅服务迁移期旧表路径；v8 southbound 主链路已经改为 ApplyExecutionPlan。
	// 因此转正条件仍必须绑定到 node-agent 上报的真实 running 状态，避免把“已接受 desired”误认为“已上线”。
	if input.Status == execution.StatusRunning &&
		nextCandidateRevisionID != "" &&
		currentDeployment.RevisionID == nextCandidateRevisionID {
		// oldCurrentRevisionID 非空时表示当前是一次替换发布；候选转正后需要释放旧 current 的 runtime 占用。
		oldCurrentRevisionID := nextCurrentRevisionID
		nextCurrentRevisionID = nextCandidateRevisionID
		nextCandidateRevisionID = ""
		nextRolloutPhase = workload.RolloutPhaseIdle
		nextRolloutMessage = fmt.Sprintf(
			"candidate revision %s passed readiness checks and became current",
			currentDeployment.RevisionID,
		)
		// 替换发布中，旧 current revision 仍可能有 running deployment 承载流量。
		// 候选实例 running 后立即 supersede 旧 execution，释放节点资源，并让 ingress 后续只发布新 current。
		if oldCurrentRevisionID != "" && oldCurrentRevisionID != currentDeployment.RevisionID {
			oldDeployment, err := loadRunningDeploymentForRevision(ctx, tx, currentDeployment.ServiceID, oldCurrentRevisionID)
			if err != nil {
				return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
			}
			if oldDeployment != nil {
				if err := supersedeRunningExecutionsByDeployment(ctx, tx, oldDeployment.ID, currentDeployment.ID); err != nil {
					return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
				}
			}
		}
	}

	// 根据 execution 终态计算 deployment/service 目标状态。
	switch input.Status {
	case execution.StatusRunning:
		targetDeploymentStatus = deployment.StatusRunning
		if nextCurrentRevisionID == "" || currentDeployment.RevisionID == nextCurrentRevisionID {
			targetServiceStatus = workload.StatusRunning
		} else {
			// 候选 revision running 但尚未晋升时，service 状态仍由 current revision 决定。
			targetServiceStatus = statusWhileHoldingCurrentRevision(nextCurrentRevisionID)
		}
	case execution.StatusFailed:
		targetDeploymentStatus = deployment.StatusFailed
		targetServiceStatus = workload.StatusFailed
		if nextCurrentRevisionID != "" && nextCurrentRevisionID != currentDeployment.RevisionID {
			// 如果还有旧 current revision 的 running deployment，则 service 可保持 degraded。
			hasFallback, err := hasRunningDeploymentForRevision(ctx, tx, currentDeployment.ServiceID, nextCurrentRevisionID, currentDeployment.ID)
			if err != nil {
				return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
			}
			if hasFallback {
				targetServiceStatus = workload.StatusDegraded
			}
		}
	}

	// 准备写回 deployment 状态。
	var reportedDeployment deployment.Deployment
	if targetDeploymentStatus != currentDeployment.Status {
		// 状态变化必须符合 deployment 状态机。
		if err := deployment.ValidateTransition(currentDeployment.Status, targetDeploymentStatus, input.Reason); err != nil {
			return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
		}
	}

	// 更新 deployment 当前状态。
	err = tx.QueryRowContext(ctx, `
		UPDATE deployments
		SET
			status = $2,
			status_reason = $3,
			updated_at = now()
		WHERE id = $1
			RETURNING id, service_id, revision_id, status, status_reason, created_at, updated_at
	`,
		currentDeployment.ID,
		targetDeploymentStatus,
		input.Reason,
	).Scan(
		&reportedDeployment.ID,
		&reportedDeployment.ServiceID,
		&reportedDeployment.RevisionID,
		&reportedDeployment.Status,
		&reportedDeployment.StatusReason,
		&reportedDeployment.CreatedAt,
		&reportedDeployment.UpdatedAt,
	)
	if err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("update deployment from execution report: %w", err)
	}

	// deployment 状态变化时记录 transition 历史。
	if targetDeploymentStatus != currentDeployment.Status {
		fromStatus := currentDeployment.Status
		if err := insertDeploymentTransition(ctx, tx, currentDeployment.ID, &fromStatus, targetDeploymentStatus, input.Reason); err != nil {
			return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, err
		}
	}

	// 更新 service 的 current/candidate revision、rollout phase 和整体状态。
	reportedService, err := updateServiceRevisionStateTx(
		ctx,
		tx,
		currentDeployment.ServiceID,
		nextCurrentRevisionID,
		nextCandidateRevisionID,
		targetServiceStatus,
		nextRolloutPhase,
		nextRolloutMessage,
	)
	if err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("update service revision state from execution report: %w", err)
	}

	// failed execution 不再占用 node 资源，需要释放 selection 中记录的 allocation。
	if input.Status == execution.StatusFailed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE nodes
			SET
				cpu_milli_allocated = GREATEST(cpu_milli_allocated - $2, 0),
				memory_mi_allocated = GREATEST(memory_mi_allocated - $3, 0),
				updated_at = now()
			WHERE id = $1
		`, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
			return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("revision node allocation after failed execution: %w", err)
		}
	}

	// 所有相关状态写入成功后提交事务。
	if err := tx.Commit(); err != nil {
		return execution.ReportAck{}, deployment.Deployment{}, workload.Service{}, fmt.Errorf("commit execution report tx: %w", err)
	}

	// 返回 ack、更新后的 deployment 和 service，供调用方记录或响应。
	return execution.ReportAck{
		Execution:  updated,
		ObservedAt: observedAt,
	}, reportedDeployment, reportedService, nil
}

// loadRunningExecutionForRevisionOnNode 查询指定 revision 在指定 node 上的 running execution。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；serviceID 是 service 唯一标识；revisionID 是 revision 唯一标识；nodeID 是 node 唯一标识；excludeDeploymentID 表示 exclude deployment 的唯一标识。
func loadRunningExecutionForRevisionOnNode(ctx context.Context, tx *sql.Tx, serviceID string, revisionID string, nodeID string, excludeDeploymentID string) (*execution.SupersededExecution, error) {
	// 没有 revision ID 时不可能存在可替换 execution。
	if revisionID == "" {
		return nil, nil
	}

	// 查询同 service/revision/node 上最新的 running execution。
	query := `
		SELECT
			d.id,
			e.id,
			e.container_id,
			e.container_name
			FROM deployments d
			JOIN deployment_executions e ON e.deployment_id = d.id
			WHERE d.service_id = $1
			  AND d.revision_id = $2
		  AND d.status = $3
		  AND e.status = $4
		  AND e.node_id = $5
	`
	args := []any{serviceID, revisionID, deployment.StatusRunning, execution.StatusRunning, nodeID}
	if excludeDeploymentID != "" {
		// 可选排除 deployment，避免把当前正在处理的 deployment 当成旧版本。
		query += ` AND d.id <> $6`
		args = append(args, excludeDeploymentID)
	}
	query += `
		ORDER BY d.created_at DESC, d.id DESC, e.created_at DESC, e.id DESC
		LIMIT 1
	`

	// 返回 node-agent 停止旧容器所需的最小信息。
	item := &execution.SupersededExecution{}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(
		&item.DeploymentID,
		&item.ExecutionID,
		&item.ContainerID,
		&item.ContainerName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有旧 execution 属于正常情况。
			return nil, nil
		}
		return nil, fmt.Errorf("load running execution for revision on node: %w", err)
	}
	return item, nil
}

// hasRunningDeploymentForRevision 判断指定 revision 是否已有 running deployment。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；serviceID 是 service 唯一标识；revisionID 是 revision 唯一标识；excludeDeploymentID 表示 exclude deployment 的唯一标识。
func hasRunningDeploymentForRevision(ctx context.Context, tx *sql.Tx, serviceID string, revisionID string, excludeDeploymentID string) (bool, error) {
	// 没有 revision ID 时直接视为不存在 running deployment。
	if revisionID == "" {
		return false, nil
	}

	// 查询该 revision 是否至少有一个 running deployment 和 running execution。
	query := `
			SELECT 1
			FROM deployments d
			JOIN deployment_executions e ON e.deployment_id = d.id
			WHERE d.service_id = $1
			  AND d.revision_id = $2
		  AND d.status = $3
		  AND e.status = $4
	`
	args := []any{serviceID, revisionID, deployment.StatusRunning, execution.StatusRunning}
	if excludeDeploymentID != "" {
		// 失败分支会排除当前 deployment，只检查是否还有其他可回退 deployment。
		query += ` AND d.id <> $5`
		args = append(args, excludeDeploymentID)
	}
	query += ` LIMIT 1`

	var marker int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有匹配行表示不存在 fallback。
			return false, nil
		}
		return false, fmt.Errorf("check running deployment for revision: %w", err)
	}
	// 查到任意一行即可确认存在 running deployment。
	return true, nil
}

// supersedeRunningExecutionsByDeployment 把指定 deployment 的 running execution 标记为 superseded 并释放节点分配。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；deploymentID 是 deployment 唯一标识；replacementDeploymentID 表示 replacement deployment 的唯一标识。
func supersedeRunningExecutionsByDeployment(ctx context.Context, tx *sql.Tx, deploymentID string, replacementDeploymentID string) error {
	// 复杂流程说明：将指定 deployment 下仍 running 的 execution 标记为 superseded。
	// 该逻辑只处理调用方确认不再承载流量的 execution，避免误杀当前 revision。
	rows, err := tx.QueryContext(ctx, `
		SELECT
			e.id,
			pd.node_id,
			pd.cpu_milli_request,
			pd.memory_mi_request
		FROM deployment_executions e
		JOIN placement_decisions pd
			ON pd.deployment_id = e.deployment_id
		WHERE e.deployment_id = $1
		  AND e.status = $2
		ORDER BY e.id ASC
		FOR UPDATE
	`, deploymentID, execution.StatusRunning)
	if err != nil {
		return fmt.Errorf("list running executions for deployment: %w", err)
	}
	defer closeRows(rows)

	// runningExecution 是当前 deployment 中仍占用节点资源、需要 supersede 的 execution。
	type runningExecution struct {
		// id 是 execution 唯一标识。
		id string
		// nodeID 是 execution 当前占用的 node 唯一标识。
		nodeID string
		// cpuMilli 是该 execution 通过 selection 占用的 CPU，单位 millicore。
		cpuMilli int
		// memoryMi 是该 execution 通过 selection 占用的内存，单位 MiB。
		memoryMi int
	}
	running := make([]runningExecution, 0)
	// 先把要处理的 execution 读入内存，避免边遍历 rows 边执行更新。
	for rows.Next() {
		var item runningExecution
		if err := rows.Scan(&item.id, &item.nodeID, &item.cpuMilli, &item.memoryMi); err != nil {
			return fmt.Errorf("scan running execution for deployment: %w", err)
		}
		running = append(running, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate running executions for deployment: %w", err)
	}
	// 没有 running execution 时无需继续更新 deployment，保持幂等。
	if len(running) == 0 {
		return nil
	}
	// replacementDeploymentID 存在时把 supersede 原因指向替代 deployment。
	reason := "rollout superseded the previous deployment"
	if replacementDeploymentID != "" {
		reason = fmt.Sprintf("superseded by deployment %s after the replacement passed readiness checks", replacementDeploymentID)
	}
	// 逐个标记 execution superseded，并释放对应 node allocation。
	for _, item := range running {
		// execution finished_at 使用当前时间，表示该 execution 不再应被视为活跃。
		if _, err := tx.ExecContext(ctx, `
			UPDATE deployment_executions
			SET
				status = $2,
				status_reason = $3,
				finished_at = $4,
				updated_at = now()
			WHERE id = $1
		`, item.id, execution.StatusSuperseded, reason, time.Now().UTC()); err != nil {
			return fmt.Errorf("mark execution superseded: %w", err)
		}
		// 使用 GREATEST 避免历史数据不一致导致 allocation 变成负数。
		if _, err := tx.ExecContext(ctx, `
			UPDATE nodes
			SET
				cpu_milli_allocated = GREATEST(cpu_milli_allocated - $2, 0),
				memory_mi_allocated = GREATEST(memory_mi_allocated - $3, 0),
				updated_at = now()
			WHERE id = $1
		`, item.nodeID, item.cpuMilli, item.memoryMi); err != nil {
			return fmt.Errorf("free node allocation for superseded execution: %w", err)
		}
	}
	// 所有 execution 释放后，把 deployment 本身标记为 superseded。
	if _, err := tx.ExecContext(ctx, `
		UPDATE deployments
		SET
			status = $2,
			status_reason = $3,
			updated_at = now()
		WHERE id = $1
	`, deploymentID, deployment.StatusSuperseded, reason); err != nil {
		return fmt.Errorf("mark deployment superseded: %w", err)
	}
	fromStatus := deployment.StatusRunning
	if err := insertDeploymentTransition(ctx, tx, deploymentID, &fromStatus, deployment.StatusSuperseded, reason); err != nil {
		return err
	}
	return nil
}

// serviceRevisionState 描述 store 模块中的 service revision state 数据。
type serviceRevisionState struct {
	// CurrentRevisionID 表示当前正式承载流量的 revision 标识。
	CurrentRevisionID string
	// CandidateRevisionID 表示正在验证但尚未提升的 candidate revision 标识。
	CandidateRevisionID string
	// Status 是资源当前状态。
	Status string
	// RolloutPhase 表示当前 rollout 状态机阶段。
	RolloutPhase string
	// RolloutMessage 表示 rollout 最近一次状态说明。
	RolloutMessage string
}

// loadLockedServiceRevisionState 加载 locked service revision state。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；serviceID 是 service 唯一标识。
func loadLockedServiceRevisionState(ctx context.Context, tx *sql.Tx, serviceID string) (serviceRevisionState, error) {
	// nullable revision ID 用 sql.NullString 扫描，避免空值和空字符串混淆。
	var state serviceRevisionState
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	// FOR UPDATE 锁定 service 行，串行化 rollout 状态变更。
	err := tx.QueryRowContext(ctx, `
		SELECT
			current_revision_id,
			candidate_revision_id,
			status,
			rollout_phase,
			rollout_message
		FROM services
		WHERE id = $1
		FOR UPDATE
	`, serviceID).Scan(
		&currentRevisionID,
		&candidateRevisionID,
		&state.Status,
		&state.RolloutPhase,
		&state.RolloutMessage,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return serviceRevisionState{}, ErrServiceNotFound
		}
		return serviceRevisionState{}, err
	}
	// 将 nullable revision 字段恢复为领域对象中的普通字符串。
	if currentRevisionID.Valid {
		state.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		state.CandidateRevisionID = candidateRevisionID.String
	}
	// rollout phase 读出后归一化，兼容数据库中的空值或旧值。
	state.RolloutPhase = workload.NormalizeRolloutPhase(state.RolloutPhase)
	return state, nil
}

// loadRunningDeploymentForRevision 查询指定 revision 最新的 running deployment。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；serviceID 是 service 唯一标识；revisionID 是 revision 唯一标识。
func loadRunningDeploymentForRevision(ctx context.Context, tx *sql.Tx, serviceID string, revisionID string) (*deployment.Deployment, error) {
	// 没有 revision ID 时不可能存在对应 deployment。
	if revisionID == "" {
		return nil, nil
	}

	// 查询指定 revision 最新的 running deployment，并加锁供调用方后续更新。
	var item deployment.Deployment
	err := tx.QueryRowContext(ctx, `
		SELECT
			id,
			service_id,
			revision_id,
			status,
			status_reason,
			created_at,
			updated_at
		FROM deployments
		WHERE service_id = $1
		  AND revision_id = $2
		  AND status = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, serviceID, revisionID, deployment.StatusRunning).Scan(
		&item.ID,
		&item.ServiceID,
		&item.RevisionID,
		&item.Status,
		&item.StatusReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有 running deployment 是调用方需要处理的正常分支。
			return nil, nil
		}
		return nil, fmt.Errorf("load running deployment for revision: %w", err)
	}
	return &item, nil
}

// updateServiceRevisionStateTx 更新 service revision state tx。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；serviceID 是 service 唯一标识；currentRevisionIDValue 是要写入的当前 revision ID；candidateRevisionIDValue 是要写入的候选 revision ID；statusValue 是要写入的 service 状态；rolloutPhase 是要写入的 rollout 阶段；rolloutMessage 是要写入的 rollout 说明。
func updateServiceRevisionStateTx(
	ctx context.Context,
	tx *sql.Tx,
	serviceID string,
	currentRevisionIDValue string,
	candidateRevisionIDValue string,
	statusValue string,
	rolloutPhase string,
	rolloutMessage string,
) (workload.Service, error) {
	// 复杂流程说明：该事务 helper 统一维护 current/candidate revision 与 rollout phase。
	// 调用方必须传入已经判断好的目标状态，这里只负责原子落库。
	// JSON 和 nullable 字段先用中间变量接收，再转换成 workload.Service。
	var updated workload.Service
	var envJSON []byte
	var commandJSON []byte
	var argsJSON []byte
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString

	// 更新 service rollout 状态，并返回 service 完整快照。
	err := tx.QueryRowContext(ctx, `
		UPDATE services
		SET
			current_revision_id = $2,
			candidate_revision_id = $3,
			status = $4,
			rollout_phase = $5,
			rollout_message = $6,
			updated_at = now()
		WHERE id = $1
			RETURNING
				id,
				name,
				display_name,
			region,
			instance_class,
			exposure,
			image,
			command_json,
			args_json,
			default_port,
			readiness_path,
			config_set_id,
			secret_set_id,
			registry_credential_id,
			env_json,
			current_revision_id,
			candidate_revision_id,
			rollout_phase,
			rollout_message,
			status,
			created_at,
			updated_at
		`, serviceID, nullableString(currentRevisionIDValue), nullableString(candidateRevisionIDValue), statusValue, workload.NormalizeRolloutPhase(rolloutPhase), rolloutMessage).Scan(
		&updated.Metadata.ID,
		&updated.Metadata.Name,
		&updated.Metadata.DisplayName,
		&updated.Spec.Region,
		&updated.Spec.InstanceClass,
		&updated.Spec.Exposure,
		&updated.Spec.Image,
		&commandJSON,
		&argsJSON,
		&updated.Spec.DefaultPort,
		&updated.Spec.ReadinessPath,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&envJSON,
		&currentRevisionID,
		&candidateRevisionID,
		&updated.Status.RolloutPhase,
		&updated.Status.RolloutMessage,
		&updated.Status.Phase,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workload.Service{}, ErrServiceNotFound
		}
		return workload.Service{}, err
	}
	// 反序列化 service 运行配置 JSON 字段。
	if err := unmarshalJSON(envJSON, &updated.Spec.Env, map[string]string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(commandJSON, &updated.Spec.Command, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &updated.Spec.Args, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	// nullable 外键字段只在数据库值有效时写回领域对象。
	if currentRevisionID.Valid {
		updated.Status.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		updated.Status.CandidateRevisionID = candidateRevisionID.String
	}
	if configSetID.Valid {
		updated.Spec.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		updated.Spec.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		updated.Spec.RegistryCredentialID = registryCredentialID.String
	}
	// 返回更新后的 service 快照。
	return updated, nil
}

// statusWhileHoldingCurrentRevision 返回保留当前稳定 revision 时应展示的 service 状态。
// 参数说明：currentRevisionID 表示 current revision 的唯一标识。
func statusWhileHoldingCurrentRevision(currentRevisionID string) string {
	if currentRevisionID != "" {
		return workload.StatusRunning
	}
	return workload.StatusDeploying
}

// GetLatestExecutionByDeployment 按 deployment 查询最新 execution。
// 参数说明：ctx 控制本次请求或后台操作生命周期；deploymentID 是 deployment 唯一标识。
func (s *Store) GetLatestExecutionByDeployment(ctx context.Context, deploymentID string) (*execution.Record, error) {
	// 按创建时间倒序读取该 deployment 最新的一条 execution。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			deployment_id,
			node_id,
			image,
			container_name,
			container_id,
			container_port,
			host_port,
			readiness_path,
			status,
			status_reason,
			started_at,
			finished_at,
			created_at,
			updated_at
		FROM deployment_executions
		WHERE deployment_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, deploymentID)

	// 扫描 execution 基础字段。
	var item execution.Record
	err := row.Scan(
		&item.ID,
		&item.DeploymentID,
		&item.NodeID,
		&item.Image,
		&item.ContainerName,
		&item.ContainerID,
		&item.ContainerPort,
		&item.HostPort,
		&item.ReadinessPath,
		&item.Status,
		&item.StatusReason,
		&item.StartedAt,
		&item.FinishedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有 execution 时返回 nil，调用方可按“尚未执行”处理。
			return nil, nil
		}
		return nil, fmt.Errorf("query latest execution by deployment: %w", err)
	}

	return &item, nil
}

// ListRunningExecutionsByDeployment 列出 deployment 下 running 且已分配 host port 的 execution。
// 参数说明：ctx 控制本次请求或后台操作生命周期；deploymentID 是 deployment 唯一标识。
func (s *Store) ListRunningExecutionsByDeployment(ctx context.Context, deploymentID string) ([]execution.Record, error) {
	// 只列出 running 且已分配 host_port 的 execution，供对外路由或观测使用。
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			deployment_id,
			node_id,
			image,
			container_name,
			container_id,
			container_port,
			host_port,
			readiness_path,
			status,
			status_reason,
			started_at,
			finished_at,
			created_at,
			updated_at
		FROM deployment_executions
		WHERE deployment_id = $1
		  AND status = $2
		  AND host_port > 0
		ORDER BY created_at ASC, id ASC
	`, deploymentID, execution.StatusRunning)
	if err != nil {
		return nil, fmt.Errorf("query running executions by deployment: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 execution 记录。
	items := make([]execution.Record, 0)
	for rows.Next() {
		var item execution.Record
		if err := rows.Scan(
			&item.ID,
			&item.DeploymentID,
			&item.NodeID,
			&item.Image,
			&item.ContainerName,
			&item.ContainerID,
			&item.ContainerPort,
			&item.HostPort,
			&item.ReadinessPath,
			&item.Status,
			&item.StatusReason,
			&item.StartedAt,
			&item.FinishedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan running execution by deployment: %w", err)
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate running executions by deployment: %w", err)
	}
	return items, nil
}

// countRunningExecutionsByDeploymentTx 统计 deployment 内 running execution 数量。
// 参数说明：ctx 控制本次请求或后台操作生命周期；tx 表示数据库事务；deploymentID 是 deployment 唯一标识。
func countRunningExecutionsByDeploymentTx(ctx context.Context, tx *sql.Tx, deploymentID string) (int, error) {
	// running 且 host_port > 0 的 execution 才计入 ready/available 副本。
	var count int
	// 在调用方事务内读取，确保计数和随后 deployment 副本数更新使用同一一致性边界。
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM deployment_executions
		WHERE deployment_id = $1
		  AND status = $2
		  AND host_port > 0
	`, deploymentID, execution.StatusRunning).Scan(&count); err != nil {
		// 包装错误时保留 deployment 维度，便于定位副本计数失败位置。
		return 0, fmt.Errorf("count running executions by deployment: %w", err)
	}
	// 返回的是当前事务视图中的 running execution 数量。
	return count, nil
}
