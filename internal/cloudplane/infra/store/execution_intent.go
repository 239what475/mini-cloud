package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	commonid "mini-cloud/internal/common/id"
	"mini-cloud/internal/common/projectedfile"
)

// ErrExecutionNotFound 表示 execution intent 记录不存在。
var ErrExecutionNotFound = errors.New("execution not found")

func (s *Store) ApplyExecutionPlan(ctx context.Context, input cloudmodel.PlanInput) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	command := input.Command
	if command == nil {
		command = []string{}
	}
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("marshal execution command: %w", err)
	}
	args := input.Args
	if args == nil {
		args = []string{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal execution args: %w", err)
	}
	env := input.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("marshal execution env: %w", err)
	}
	projectedFiles := projectedfile.CloneFiles(input.ProjectedFiles)
	if projectedFiles == nil {
		projectedFiles = []projectedfile.File{}
	}
	projectedFilesJSON, err := json.Marshal(projectedFiles)
	if err != nil {
		return "", fmt.Errorf("marshal execution projected files: %w", err)
	}
	var credentialServer sql.NullString
	var credentialUsername sql.NullString
	var credentialPassword sql.NullString
	if input.ImageCredential != nil {
		if input.ImageCredential.Server != "" {
			credentialServer = sql.NullString{String: input.ImageCredential.Server, Valid: true}
		}
		if input.ImageCredential.Username != "" {
			credentialUsername = sql.NullString{String: input.ImageCredential.Username, Valid: true}
		}
		if input.ImageCredential.Password != "" {
			credentialPassword = sql.NullString{String: input.ImageCredential.Password, Valid: true}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin apply execution plan tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $2,
			status_reason = $3,
			finished_at = CASE WHEN finished_at IS NULL THEN now() ELSE finished_at END,
			updated_at = now()
		WHERE service_id = $1
		  AND plan_id <> $4
		  AND status IN ($5, $6)
	`, input.ServiceID, cloudmodel.StatusSuperseded, "superseded by a newer execution plan", input.PlanID, cloudmodel.StatusPending, cloudmodel.StatusDeploying); err != nil {
		return "", fmt.Errorf("supersede old execution intents: %w", err)
	}

	id, err := commonid.New("exe")
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
			INSERT INTO execution_intents (
				id,
				work_action,
				plan_id,
				service_id,
				service_name,
				service_exposure,
				service_generation,
				image,
				command_json,
				args_json,
				env_json,
				projected_files_json,
				image_credential_server,
				image_credential_username,
				image_credential_password,
				container_port,
				readiness_path,
				cpu_milli_request,
				memory_mi_request,
				status,
				status_reason
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
			ON CONFLICT (plan_id) DO UPDATE
			SET
				work_action = EXCLUDED.work_action,
				service_name = EXCLUDED.service_name,
				service_exposure = EXCLUDED.service_exposure,
				service_generation = EXCLUDED.service_generation,
				image = EXCLUDED.image,
				command_json = EXCLUDED.command_json,
				args_json = EXCLUDED.args_json,
				env_json = EXCLUDED.env_json,
				projected_files_json = EXCLUDED.projected_files_json,
				image_credential_server = EXCLUDED.image_credential_server,
				image_credential_username = EXCLUDED.image_credential_username,
				image_credential_password = EXCLUDED.image_credential_password,
				container_port = EXCLUDED.container_port,
				readiness_path = EXCLUDED.readiness_path,
				cpu_milli_request = EXCLUDED.cpu_milli_request,
				memory_mi_request = EXCLUDED.memory_mi_request,
				updated_at = now()
		`,
		id,
		cloudmodel.WorkActionRun,
		input.PlanID,
		input.ServiceID,
		input.ServiceName,
		input.Exposure,
		input.ServiceGeneration,
		input.Image,
		commandJSON,
		argsJSON,
		envJSON,
		projectedFilesJSON,
		credentialServer,
		credentialUsername,
		credentialPassword,
		input.ContainerPort,
		input.ReadinessPath,
		input.CPUMilliRequest,
		input.MemoryMiRequest,
		cloudmodel.StatusPending,
		"execution plan accepted",
	); err != nil {
		return "", fmt.Errorf("upsert execution intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit apply execution plan tx: %w", err)
	}
	return input.PlanID, nil
}

func (s *Store) DeleteExecutionPlansForService(ctx context.Context, input cloudmodel.DeletePlanInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete execution plan tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $2,
			status_reason = $3,
			finished_at = CASE WHEN finished_at IS NULL THEN now() ELSE finished_at END,
			updated_at = now()
		WHERE service_id = $1
		  AND work_action = $5
		  AND status = $4
	`, input.ServiceID, cloudmodel.StatusSuperseded, "service deletion requested before execution started", cloudmodel.StatusPending, cloudmodel.WorkActionRun); err != nil {
		return fmt.Errorf("supersede unstarted execution intents for delete: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		WITH locked AS (
			SELECT id, updated_at
			FROM execution_intents
			WHERE service_id = $1
			  AND work_action = $7
			  AND status = $8
			  AND node_id IS NOT NULL
			  AND container_id <> ''
			FOR UPDATE
		)
		UPDATE execution_intents
		SET
			work_action = $2,
			plan_id = $3,
			service_generation = $4,
			status = $5,
			status_reason = $6,
			started_at = NULL,
			finished_at = NULL,
			updated_at = now()
		FROM locked
		WHERE execution_intents.id = locked.id
	`, input.ServiceID, cloudmodel.WorkActionDelete, input.PlanID, input.ServiceGeneration, cloudmodel.StatusPending, "service deletion requested by control-plane", cloudmodel.WorkActionRun, cloudmodel.StatusRunning); err != nil {
		return fmt.Errorf("mark running execution intents for delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete execution plan tx: %w", err)
	}
	return nil
}

func (s *Store) ListIngressRouteSources(ctx context.Context) ([]cloudmodel.RouteSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest_plan AS (
			SELECT DISTINCT ON (service_id)
				service_id,
				service_name,
				service_exposure,
				plan_id
			FROM execution_intents
			WHERE status <> $2
			ORDER BY service_id, service_generation DESC, updated_at DESC, plan_id DESC
		)
		SELECT
			p.service_name,
			e.node_id,
			COALESCE(e.host_port, 0),
			(e.id IS NOT NULL) AS has_backend
		FROM latest_plan p
		LEFT JOIN execution_intents e
			ON e.plan_id = p.plan_id
		   AND e.status = $1
		   AND e.node_id IS NOT NULL
		   AND e.host_port > 0
		WHERE p.service_exposure = 'public'
		ORDER BY p.service_name ASC, p.plan_id ASC, e.id ASC
	`, cloudmodel.StatusRunning, cloudmodel.StatusSuperseded)
	if err != nil {
		return nil, fmt.Errorf("query ingress route sources: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.RouteSource, 0)
	for rows.Next() {
		var item cloudmodel.RouteSource
		var nodeID sql.NullString
		if err := rows.Scan(&item.ServiceName, &nodeID, &item.HostPort, &item.HasBackend); err != nil {
			return nil, fmt.Errorf("scan ingress route source: %w", err)
		}
		if nodeID.Valid {
			item.NodeID = nodeID.String
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ingress route sources: %w", err)
	}
	return items, nil
}

func (s *Store) ListExecutionSnapshots(ctx context.Context) ([]cloudmodel.ExecutionSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest_reason AS (
			SELECT DISTINCT ON (plan_id)
				plan_id,
				status_reason,
				updated_at
			FROM execution_intents
			ORDER BY plan_id, updated_at DESC, id DESC
		)
		SELECT
			e.plan_id,
			e.service_id,
			e.service_name,
			e.service_generation,
			e.status,
			COALESCE(l.status_reason, '') AS last_status_reason,
			e.updated_at AS observed_at
		FROM execution_intents e
		LEFT JOIN latest_reason l ON l.plan_id = e.plan_id
		ORDER BY e.updated_at DESC, e.plan_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query execution snapshots: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.ExecutionSnapshot, 0)
	for rows.Next() {
		var item cloudmodel.ExecutionSnapshot
		if err := rows.Scan(
			&item.PlanID,
			&item.ServiceID,
			&item.ServiceName,
			&item.ServiceGeneration,
			&item.Status,
			&item.LastStatusReason,
			&item.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan execution snapshot: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution snapshots: %w", err)
	}
	return items, nil
}

func (s *Store) CreateExecutionClaim(ctx context.Context, nodeID string) (*cloudmodel.WorkItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin execution claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var schedulable bool
	var nodeStatus string
	var cpuMilliAllocatable int
	var memoryMiAllocatable int
	var cpuMilliAllocated int
	var memoryMiAllocated int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, schedulable, cpu_milli_allocatable, memory_mi_allocatable, cpu_milli_allocated, memory_mi_allocated
		FROM nodes
		WHERE id = $1
		FOR UPDATE
	`, nodeID).Scan(&nodeStatus, &schedulable, &cpuMilliAllocatable, &memoryMiAllocatable, &cpuMilliAllocated, &memoryMiAllocated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load node for execution claim: %w", err)
	}
	if nodeStatus != cloudmodel.StatusReady {
		return nil, nil
	}

	var work cloudmodel.WorkItem
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var projectedFilesJSON []byte
	var credentialServer sql.NullString
	var credentialUsername sql.NullString
	var credentialPassword sql.NullString
	var cpuMilliRequest int
	var memoryMiRequest int
	var serviceGeneration int64
	var storedNodeID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT
			id,
			work_action,
			plan_id,
			node_id,
			service_id,
			service_name,
			service_generation,
			image,
			command_json,
			args_json,
			env_json,
			projected_files_json,
			image_credential_server,
			image_credential_username,
			image_credential_password,
			container_port,
			readiness_path,
			container_name,
			container_id,
			host_port,
			cpu_milli_request,
			memory_mi_request
		FROM execution_intents
		WHERE status = $1
		  AND (
			(work_action = $2 AND node_id = $3)
			OR (
				work_action = $4
				AND $5
				AND cpu_milli_request <= $6
				AND memory_mi_request <= $7
			)
		  )
		ORDER BY CASE WHEN work_action = $2 THEN 0 ELSE 1 END, created_at ASC, plan_id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`,
		cloudmodel.StatusPending,
		cloudmodel.WorkActionDelete,
		nodeID,
		cloudmodel.WorkActionRun,
		schedulable,
		cpuMilliAllocatable-cpuMilliAllocated,
		memoryMiAllocatable-memoryMiAllocated,
	).Scan(
		&work.ExecutionID,
		&work.Action,
		&work.PlanID,
		&storedNodeID,
		&work.ServiceID,
		&work.ServiceName,
		&serviceGeneration,
		&work.Image,
		&commandJSON,
		&argsJSON,
		&envJSON,
		&projectedFilesJSON,
		&credentialServer,
		&credentialUsername,
		&credentialPassword,
		&work.ContainerPort,
		&work.ReadinessPath,
		&work.ContainerName,
		&work.ContainerID,
		&work.HostPort,
		&cpuMilliRequest,
		&memoryMiRequest,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load claimable execution intent: %w", err)
	}
	work.Action = cloudmodel.NormalizeWorkAction(work.Action)
	work.NodeID = nodeID
	if storedNodeID.Valid {
		work.NodeID = storedNodeID.String
	}
	if work.Action == cloudmodel.WorkActionDelete {
		startedAt := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `
			UPDATE execution_intents
			SET
				status = $2,
				status_reason = $3,
				started_at = $4,
				updated_at = now()
			WHERE id = $1
		`, work.ExecutionID, cloudmodel.StatusDeploying, fmt.Sprintf("agent on node %s claimed delete execution intent", nodeID), startedAt); err != nil {
			return nil, fmt.Errorf("mark delete execution intent deploying: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit delete execution claim tx: %w", err)
		}
		return &work, nil
	}
	work.Command = []string{}
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &work.Command); err != nil {
			return nil, fmt.Errorf("decode execution command: %w", err)
		}
	}
	if work.Command == nil {
		work.Command = []string{}
	}
	work.Args = []string{}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &work.Args); err != nil {
			return nil, fmt.Errorf("decode execution args: %w", err)
		}
	}
	if work.Args == nil {
		work.Args = []string{}
	}
	work.Env = map[string]string{}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &work.Env); err != nil {
			return nil, fmt.Errorf("decode execution env: %w", err)
		}
	}
	if work.Env == nil {
		work.Env = map[string]string{}
	}
	work.ProjectedFiles = []projectedfile.File{}
	if len(projectedFilesJSON) > 0 {
		if err := json.Unmarshal(projectedFilesJSON, &work.ProjectedFiles); err != nil {
			return nil, fmt.Errorf("decode execution projected files: %w", err)
		}
	}
	work.ProjectedFiles = projectedfile.CloneFiles(work.ProjectedFiles)
	if work.ProjectedFiles == nil {
		work.ProjectedFiles = []projectedfile.File{}
	}
	if credentialServer.Valid {
		work.ImageCredential = &cloudmodel.ImageCredential{
			Server:   credentialServer.String,
			Username: credentialUsername.String,
			Password: credentialPassword.String,
		}
	}
	superseded, err := loadRunningIntentForServiceOnNode(ctx, tx, work.ServiceID, work.PlanID, nodeID)
	if err != nil {
		return nil, err
	}
	work.SupersededExecution = superseded
	work.ContainerName = fmt.Sprintf("mini-cloud-%s", work.PlanID)

	startedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			node_id = $2,
			container_name = $3,
			status = $4,
			status_reason = $5,
			started_at = $6,
			updated_at = now()
		WHERE id = $1
	`, work.ExecutionID, nodeID, work.ContainerName, cloudmodel.StatusDeploying, fmt.Sprintf("agent on node %s claimed execution intent", nodeID), startedAt); err != nil {
		return nil, fmt.Errorf("mark execution intent deploying: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocated = cpu_milli_allocated + $2,
			memory_mi_allocated = memory_mi_allocated + $3,
			updated_at = now()
		WHERE id = $1
	`, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
		return nil, fmt.Errorf("reserve node allocation for execution intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit execution claim tx: %w", err)
	}
	return &work, nil
}

func (s *Store) UpdateExecutionFromNodeReport(ctx context.Context, nodeID string, executionID string, input cloudmodel.ReportInput) (cloudmodel.ReportAck, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.ReportAck{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.ReportAck{}, fmt.Errorf("begin report execution intent tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, cpuMilliRequest, memoryMiRequest, err := loadExecutionIntentRecord(ctx, tx, nodeID, executionID)
	if err != nil {
		return cloudmodel.ReportAck{}, err
	}
	if current.Status != cloudmodel.StatusDeploying {
		if current.Status == input.Status {
			return cloudmodel.ReportAck{Execution: current, ObservedAt: time.Now().UTC()}, nil
		}
		return cloudmodel.ReportAck{}, fmt.Errorf("execution %s is already %s and cannot transition to %s", executionID, current.Status, input.Status)
	}

	observedAt := time.Now().UTC()
	var finishedAt sql.NullTime
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSuperseded {
		finishedAt = sql.NullTime{Time: observedAt, Valid: true}
	}
	var updated cloudmodel.Record
	err = tx.QueryRowContext(ctx, `
		UPDATE execution_intents
		SET
			container_name = $2,
			container_id = $3,
			host_port = $4,
			status = $5,
			status_reason = $6,
			finished_at = $7,
			updated_at = now()
		WHERE id = $1
		RETURNING id, plan_id, node_id, image, container_name, container_id, container_port, host_port, readiness_path, status, status_reason, started_at, finished_at, created_at, updated_at
	`, executionID, input.ContainerName, input.ContainerID, input.HostPort, input.Status, input.Reason, finishedAt).Scan(
		&updated.ID,
		&updated.PlanID,
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
		return cloudmodel.ReportAck{}, fmt.Errorf("update execution intent report: %w", err)
	}
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSuperseded {
		if err := freeNodeAllocation(ctx, tx, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
			return cloudmodel.ReportAck{}, err
		}
	}
	if input.SupersededExecutionID != "" {
		if err := supersedeExecutionIntent(ctx, tx, nodeID, input.SupersededExecutionID, "superseded by replacement execution"); err != nil {
			return cloudmodel.ReportAck{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return cloudmodel.ReportAck{}, fmt.Errorf("commit report execution intent tx: %w", err)
	}
	return cloudmodel.ReportAck{Execution: updated, ObservedAt: observedAt}, nil
}

func loadExecutionIntentRecord(ctx context.Context, tx *sql.Tx, nodeID string, executionID string) (cloudmodel.Record, int, int, error) {
	var current cloudmodel.Record
	var cpuMilliRequest int
	var memoryMiRequest int
	err := tx.QueryRowContext(ctx, `
		SELECT id, plan_id, node_id, image, container_name, container_id, container_port, host_port, readiness_path, status, status_reason, started_at, finished_at, created_at, updated_at, cpu_milli_request, memory_mi_request
		FROM execution_intents
		WHERE id = $1
		  AND node_id = $2
		FOR UPDATE
	`, executionID, nodeID).Scan(
		&current.ID,
		&current.PlanID,
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
		&cpuMilliRequest,
		&memoryMiRequest,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Record{}, 0, 0, ErrExecutionNotFound
		}
		return cloudmodel.Record{}, 0, 0, fmt.Errorf("load execution intent: %w", err)
	}
	return current, cpuMilliRequest, memoryMiRequest, nil
}

func loadRunningIntentForServiceOnNode(ctx context.Context, tx *sql.Tx, serviceID string, planID string, nodeID string) (*cloudmodel.SupersededExecution, error) {
	var item cloudmodel.SupersededExecution
	err := tx.QueryRowContext(ctx, `
		SELECT plan_id, id, container_id, container_name
		FROM execution_intents
		WHERE service_id = $1
		  AND plan_id <> $2
		  AND node_id = $3
		  AND status = $4
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, serviceID, planID, nodeID, cloudmodel.StatusRunning).Scan(&item.PlanID, &item.ExecutionID, &item.ContainerID, &item.ContainerName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load running execution intent for service: %w", err)
	}
	return &item, nil
}

func supersedeExecutionIntent(ctx context.Context, tx *sql.Tx, nodeID string, executionID string, reason string) error {
	current, cpuMilliRequest, memoryMiRequest, err := loadExecutionIntentRecord(ctx, tx, nodeID, executionID)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			return nil
		}
		return err
	}
	if current.Status != cloudmodel.StatusRunning && current.Status != cloudmodel.StatusDeploying {
		return nil
	}
	finishedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $2,
			status_reason = $3,
			finished_at = $4,
			updated_at = now()
		WHERE id = $1
	`, executionID, cloudmodel.StatusSuperseded, reason, finishedAt); err != nil {
		return fmt.Errorf("supersede execution intent: %w", err)
	}
	return freeNodeAllocation(ctx, tx, nodeID, cpuMilliRequest, memoryMiRequest)
}

func freeNodeAllocation(ctx context.Context, tx *sql.Tx, nodeID string, cpuMilliRequest int, memoryMiRequest int) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocated = GREATEST(cpu_milli_allocated - $2, 0),
			memory_mi_allocated = GREATEST(memory_mi_allocated - $3, 0),
			updated_at = now()
		WHERE id = $1
	`, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
		return fmt.Errorf("free node allocation for execution intent: %w", err)
	}
	return nil
}
