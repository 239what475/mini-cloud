package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

var ErrExecutionNotFound = errors.New("execution not found")

type executionIntentInput struct {
	IntentKey         string
	ServiceID         string
	ServiceName       string
	ServiceGeneration int64
	Image             string
	Command           []string
	Args              []string
	Env               map[string]string
	ContainerPort     int
	ReadinessPath     string
	CPUMilliRequest   int
	MemoryMiRequest   int
	Exposure          string
}

type serviceDeleteIntentInput struct {
	ServiceID         string
	ServiceGeneration int64
	IntentKey         string
}

func (in executionIntentInput) validate() error {
	if in.IntentKey == "" {
		return errors.New("intentKey is required")
	}
	if in.ServiceID == "" {
		return errors.New("serviceID is required")
	}
	if in.ServiceName == "" {
		return errors.New("serviceName is required")
	}
	if in.Image == "" {
		return errors.New("image is required")
	}
	if in.ContainerPort <= 0 || in.ContainerPort > 65535 {
		return errors.New("containerPort must be between 1 and 65535")
	}
	if in.CPUMilliRequest <= 0 || in.MemoryMiRequest <= 0 {
		return errors.New("cpuMilliRequest and memoryMiRequest must be greater than 0")
	}
	if in.Exposure != cloudmodel.ExposurePublic && in.Exposure != cloudmodel.ExposurePrivate {
		return errors.New("exposure must be public or private")
	}
	return nil
}

func (in serviceDeleteIntentInput) validate() error {
	if in.ServiceID == "" {
		return errors.New("serviceID is required")
	}
	if in.IntentKey == "" {
		return errors.New("intentKey is required")
	}
	if in.ServiceGeneration <= 0 {
		return errors.New("serviceGeneration must be greater than 0")
	}
	return nil
}

func upsertServiceRunIntentTx(ctx context.Context, tx *sql.Tx, input executionIntentInput) (string, error) {
	if err := input.validate(); err != nil {
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

	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $2,
			status_reason = $3,
			finished_at = CASE WHEN finished_at IS NULL THEN now() ELSE finished_at END,
			updated_at = now()
		WHERE service_id = $1
		  AND intent_key <> $4
		  AND work_action = $5
		  AND status IN ($6, $7)
	`, input.ServiceID, cloudmodel.StatusFailed, "stopped before startup by newer service generation", input.IntentKey, cloudmodel.WorkActionRun, cloudmodel.StatusPending, cloudmodel.StatusDeploying); err != nil {
		return "", fmt.Errorf("fail old unstarted execution intents: %w", err)
	}

	id, err := newID("exe")
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			work_action = $2,
			intent_key = $3,
			service_generation = $4,
			status = $5,
			status_reason = $6,
			started_at = NULL,
			finished_at = NULL,
			updated_at = now()
		WHERE service_id = $1
		  AND intent_key <> $3
		  AND work_action = $7
		  AND status = $8
		  AND node_id IS NOT NULL
		  AND container_id <> ''
	`, input.ServiceID, cloudmodel.WorkActionDelete, replacementDeleteIntentKey(input.ServiceID, input.ServiceGeneration), input.ServiceGeneration, cloudmodel.StatusPending, "new service generation requested; stopping previous container", cloudmodel.WorkActionRun, cloudmodel.StatusRunning); err != nil {
		return "", fmt.Errorf("mark old running execution intents for replacement delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_intents (
			id,
			work_action,
			intent_key,
			service_id,
			service_name,
			service_exposure,
			service_generation,
			image,
			command_json,
			args_json,
			env_json,
			container_port,
			readiness_path,
			cpu_milli_request,
			memory_mi_request,
			status,
			status_reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (intent_key) DO UPDATE
		SET
			work_action = EXCLUDED.work_action,
			service_name = EXCLUDED.service_name,
			service_exposure = EXCLUDED.service_exposure,
			service_generation = EXCLUDED.service_generation,
			image = EXCLUDED.image,
			command_json = EXCLUDED.command_json,
			args_json = EXCLUDED.args_json,
			env_json = EXCLUDED.env_json,
			container_port = EXCLUDED.container_port,
			readiness_path = EXCLUDED.readiness_path,
			cpu_milli_request = EXCLUDED.cpu_milli_request,
			memory_mi_request = EXCLUDED.memory_mi_request,
			node_id = CASE WHEN execution_intents.status = $18 THEN NULL ELSE execution_intents.node_id END,
			container_name = CASE WHEN execution_intents.status = $18 THEN '' ELSE execution_intents.container_name END,
			container_id = CASE WHEN execution_intents.status = $18 THEN '' ELSE execution_intents.container_id END,
			host_port = CASE WHEN execution_intents.status = $18 THEN 0 ELSE execution_intents.host_port END,
			status = CASE WHEN execution_intents.status = $18 THEN EXCLUDED.status ELSE execution_intents.status END,
			status_reason = CASE WHEN execution_intents.status = $18 THEN EXCLUDED.status_reason ELSE execution_intents.status_reason END,
			started_at = CASE WHEN execution_intents.status = $18 THEN NULL ELSE execution_intents.started_at END,
			finished_at = CASE WHEN execution_intents.status = $18 THEN NULL ELSE execution_intents.finished_at END,
			updated_at = now()
	`,
		id,
		cloudmodel.WorkActionRun,
		input.IntentKey,
		input.ServiceID,
		input.ServiceName,
		input.Exposure,
		input.ServiceGeneration,
		input.Image,
		commandJSON,
		argsJSON,
		envJSON,
		input.ContainerPort,
		input.ReadinessPath,
		input.CPUMilliRequest,
		input.MemoryMiRequest,
		cloudmodel.StatusPending,
		"execution intent accepted",
		cloudmodel.StatusFailed,
	); err != nil {
		return "", fmt.Errorf("upsert execution intent: %w", err)
	}
	return input.IntentKey, nil
}

func createServiceDeleteIntentTx(ctx context.Context, tx *sql.Tx, input serviceDeleteIntentInput) error {
	if err := input.validate(); err != nil {
		return err
	}

	var deleteIntentExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM execution_intents
			WHERE intent_key = $1
			  AND work_action = $2
		)
	`, input.IntentKey, cloudmodel.WorkActionDelete).Scan(&deleteIntentExists); err != nil {
		return fmt.Errorf("check delete service intent: %w", err)
	}
	if deleteIntentExists {
		return nil
	}

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
	`, input.ServiceID, cloudmodel.StatusFailed, "service deletion requested before execution started", cloudmodel.StatusPending, cloudmodel.WorkActionRun); err != nil {
		return fmt.Errorf("fail unstarted execution intents for delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		WITH locked AS (
			SELECT id, node_id, cpu_milli_request, memory_mi_request
			FROM execution_intents
			WHERE service_id = $1
			  AND work_action = $5
			  AND status = $4
			  AND container_id = ''
			  AND node_id IS NOT NULL
			FOR UPDATE
		),
		updated AS (
			UPDATE execution_intents
			SET
				status = $2,
				status_reason = $3,
				finished_at = CASE WHEN finished_at IS NULL THEN now() ELSE finished_at END,
				updated_at = now()
			FROM locked
			WHERE execution_intents.id = locked.id
			RETURNING locked.node_id, locked.cpu_milli_request, locked.memory_mi_request
		),
		freed AS (
			SELECT
				node_id,
				SUM(cpu_milli_request) AS cpu_milli,
				SUM(memory_mi_request) AS memory_mi
			FROM updated
			GROUP BY node_id
		)
		UPDATE nodes
		SET
			cpu_milli_allocated = GREATEST(cpu_milli_allocated - freed.cpu_milli, 0),
			memory_mi_allocated = GREATEST(memory_mi_allocated - freed.memory_mi, 0),
			updated_at = now()
		FROM freed
		WHERE nodes.id = freed.node_id
		`, input.ServiceID, cloudmodel.StatusFailed, "service deletion requested before container was created", cloudmodel.StatusDeploying, cloudmodel.WorkActionRun); err != nil {
		return fmt.Errorf("fail containerless deploying execution intents for delete: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
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
			intent_key = $3,
			service_generation = $4,
			status = $5,
			status_reason = $6,
			started_at = NULL,
			finished_at = NULL,
			updated_at = now()
		FROM locked
		WHERE execution_intents.id = locked.id
	`, input.ServiceID, cloudmodel.WorkActionDelete, input.IntentKey, input.ServiceGeneration, cloudmodel.StatusPending, "service deletion requested by control-plane", cloudmodel.WorkActionRun, cloudmodel.StatusRunning)
	if err != nil {
		return fmt.Errorf("mark running execution intents for delete: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delete execution affected rows: %w", err)
	}
	if rowsAffected == 0 {
		if err := insertCompletedServiceDeleteSnapshot(ctx, tx, input); err != nil {
			return err
		}
	}
	return nil
}

func insertCompletedServiceDeleteSnapshot(ctx context.Context, tx *sql.Tx, input serviceDeleteIntentInput) error {
	var serviceName string
	var serviceExposure string
	var image string
	var containerPort int
	var readinessPath string
	var cpuMilliRequest int
	var memoryMiRequest int
	err := tx.QueryRowContext(ctx, `
		SELECT
			service_name,
			service_exposure,
			image,
			container_port,
			readiness_path,
			cpu_milli_request,
			memory_mi_request
		FROM execution_intents
		WHERE service_id = $1
		ORDER BY service_generation DESC, updated_at DESC, id DESC
		LIMIT 1
	`, input.ServiceID).Scan(&serviceName, &serviceExposure, &image, &containerPort, &readinessPath, &cpuMilliRequest, &memoryMiRequest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load service execution metadata for delete snapshot: %w", err)
	}
	id, err := newID("exe")
	if err != nil {
		return fmt.Errorf("generate delete service delete snapshot id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_intents (
			id,
			work_action,
			intent_key,
			service_id,
			service_name,
			service_exposure,
			service_generation,
			image,
			container_port,
			readiness_path,
			cpu_milli_request,
			memory_mi_request,
			status,
			status_reason,
			finished_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now(), now(), now())
		ON CONFLICT (intent_key) DO UPDATE
		SET
			status = EXCLUDED.status,
			status_reason = EXCLUDED.status_reason,
			finished_at = COALESCE(execution_intents.finished_at, EXCLUDED.finished_at),
			updated_at = now()
	`, id, cloudmodel.WorkActionDelete, input.IntentKey, input.ServiceID, serviceName, serviceExposure, input.ServiceGeneration, image, containerPort, readinessPath, cpuMilliRequest, memoryMiRequest, cloudmodel.StatusSucceeded, "service had no running container to delete"); err != nil {
		return fmt.Errorf("insert completed delete service delete snapshot: %w", err)
	}
	return nil
}

func (s *Store) ListIngressRouteSources(ctx context.Context) ([]cloudmodel.RouteSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			services.name,
			services.host,
			e.node_id,
			COALESCE(e.host_port, 0),
			(e.id IS NOT NULL) AS has_backend
		FROM services
		LEFT JOIN execution_intents e
			ON e.service_id = services.id
		   AND e.service_generation = services.generation
		   AND e.work_action = $2
		   AND e.status = $1
		   AND e.node_id IS NOT NULL
		   AND e.host_port > 0
		WHERE services.desired_state = $3
		  AND services.exposure = $4
		ORDER BY services.name ASC, services.id ASC, e.id ASC
	`, cloudmodel.StatusRunning, cloudmodel.WorkActionRun, cloudmodel.ServiceDesiredActive, cloudmodel.ExposurePublic)
	if err != nil {
		return nil, fmt.Errorf("query ingress route sources: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.RouteSource, 0)
	for rows.Next() {
		var item cloudmodel.RouteSource
		var nodeID sql.NullString
		if err := rows.Scan(&item.ServiceName, &item.Host, &nodeID, &item.HostPort, &item.HasBackend); err != nil {
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
		SELECT
			intent_key,
			service_id,
			service_name,
			service_generation,
			status,
			status_reason,
			updated_at AS observed_at
		FROM execution_intents
		ORDER BY updated_at DESC, intent_key ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query execution snapshots: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.ExecutionSnapshot, 0)
	for rows.Next() {
		var item cloudmodel.ExecutionSnapshot
		if err := rows.Scan(
			&item.IntentKey,
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

func (s *Store) MarkExecutionIntentFailed(ctx context.Context, intentKey string, reason string) error {
	if intentKey == "" {
		return errors.New("intentKey is required")
	}
	if reason == "" {
		return errors.New("reason is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $2,
			status_reason = $3,
			finished_at = COALESCE(finished_at, now()),
			updated_at = now()
		WHERE intent_key = $1
		  AND status = $4
	`, intentKey, cloudmodel.StatusFailed, reason, cloudmodel.StatusPending); err != nil {
		return fmt.Errorf("mark execution intent failed: %w", err)
	}
	return nil
}

func (s *Store) MarkPendingExecutionFailedForProvisioningNode(ctx context.Context, nodeNamePrefix string, nodeName string, reason string) error {
	if nodeNamePrefix == "" || nodeName == "" {
		return errors.New("name is required")
	}
	if reason == "" {
		return errors.New("reason is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE execution_intents
		SET
			status = $4,
			status_reason = $5,
			finished_at = COALESCE(finished_at, now()),
			updated_at = now()
		WHERE work_action = $1
		  AND status = $2
		  AND $3 = $6 || '-' || lower(substr(md5(intent_key), 1, 10))
	`, cloudmodel.WorkActionRun, cloudmodel.StatusPending, nodeName, cloudmodel.StatusFailed, reason, nodeNamePrefix); err != nil {
		return fmt.Errorf("mark pending execution failed for provisioning node: %w", err)
	}
	return nil
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
	var cpuMilliRequest int
	var memoryMiRequest int
	var serviceGeneration int64
	var storedNodeID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT
			id,
			work_action,
			intent_key,
			node_id,
			service_id,
			service_name,
			service_generation,
			image,
			command_json,
			args_json,
			env_json,
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
				AND NOT EXISTS (
					SELECT 1
					FROM execution_intents delete_work
					WHERE delete_work.service_id = execution_intents.service_id
					  AND delete_work.work_action = $9
					  AND delete_work.status IN ($1, $10)
				)
				AND (
					EXISTS (
						SELECT 1
						FROM execution_intents running
						WHERE running.service_id = execution_intents.service_id
						  AND running.status = $8
						  AND running.node_id = $3
					)
					OR (
						NOT EXISTS (
							SELECT 1
							FROM execution_intents running
							WHERE running.service_id = execution_intents.service_id
							  AND running.status = $8
						)
						AND cpu_milli_request <= $6
						AND memory_mi_request <= $7
					)
				)
			)
		)
		ORDER BY CASE WHEN work_action = $2 THEN 0 ELSE 1 END, created_at ASC, intent_key ASC
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
		cloudmodel.StatusRunning,
		cloudmodel.WorkActionDelete,
		cloudmodel.StatusDeploying,
	).Scan(
		&work.ExecutionID,
		&work.Action,
		&work.IntentKey,
		&storedNodeID,
		&work.ServiceID,
		&work.ServiceName,
		&serviceGeneration,
		&work.Image,
		&commandJSON,
		&argsJSON,
		&envJSON,
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
	work.ContainerName = fmt.Sprintf("mini-cloud-%s", work.IntentKey)

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

	current, err := loadExecutionIntentRecord(ctx, tx, nodeID, executionID)
	if err != nil {
		return cloudmodel.ReportAck{}, err
	}
	if current.Execution.Status != cloudmodel.StatusDeploying {
		if current.Execution.Status == input.Status {
			return cloudmodel.ReportAck{Execution: current.Execution, ObservedAt: time.Now().UTC()}, nil
		}
		return cloudmodel.ReportAck{}, fmt.Errorf("execution %s is already %s and cannot transition to %s", executionID, current.Execution.Status, input.Status)
	}

	observedAt := time.Now().UTC()
	var finishedAt sql.NullTime
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSucceeded {
		finishedAt = sql.NullTime{Time: observedAt, Valid: true}
	}
	var updated cloudmodel.ExecutionRecord
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
		RETURNING id, intent_key, node_id, image, container_name, container_id, container_port, host_port, readiness_path, status, status_reason, started_at, finished_at, created_at, updated_at
	`, executionID, input.ContainerName, input.ContainerID, input.HostPort, input.Status, input.Reason, finishedAt).Scan(
		&updated.ID,
		&updated.IntentKey,
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
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSucceeded {
		if err := freeNodeAllocation(ctx, tx, nodeID, current.CPUMilliRequest, current.MemoryMiRequest); err != nil {
			return cloudmodel.ReportAck{}, err
		}
	}
	if current.WorkAction == cloudmodel.WorkActionDelete && input.Status == cloudmodel.StatusSucceeded {
		if err := deleteServiceTruthAfterDeleteExecution(ctx, tx, current.ServiceID, current.ServiceGeneration); err != nil {
			return cloudmodel.ReportAck{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return cloudmodel.ReportAck{}, fmt.Errorf("commit report execution intent tx: %w", err)
	}
	return cloudmodel.ReportAck{Execution: updated, ObservedAt: observedAt}, nil
}

type executionIntentRecord struct {
	Execution         cloudmodel.ExecutionRecord
	WorkAction        string
	ServiceID         string
	ServiceGeneration int64
	CPUMilliRequest   int
	MemoryMiRequest   int
}

func loadExecutionIntentRecord(ctx context.Context, tx *sql.Tx, nodeID string, executionID string) (executionIntentRecord, error) {
	var current executionIntentRecord
	err := tx.QueryRowContext(ctx, `
		SELECT
			id,
			work_action,
			service_id,
			service_generation,
			intent_key,
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
			updated_at,
			cpu_milli_request,
			memory_mi_request
		FROM execution_intents
		WHERE id = $1
		  AND node_id = $2
		FOR UPDATE
	`, executionID, nodeID).Scan(
		&current.Execution.ID,
		&current.WorkAction,
		&current.ServiceID,
		&current.ServiceGeneration,
		&current.Execution.IntentKey,
		&current.Execution.NodeID,
		&current.Execution.Image,
		&current.Execution.ContainerName,
		&current.Execution.ContainerID,
		&current.Execution.ContainerPort,
		&current.Execution.HostPort,
		&current.Execution.ReadinessPath,
		&current.Execution.Status,
		&current.Execution.StatusReason,
		&current.Execution.StartedAt,
		&current.Execution.FinishedAt,
		&current.Execution.CreatedAt,
		&current.Execution.UpdatedAt,
		&current.CPUMilliRequest,
		&current.MemoryMiRequest,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return executionIntentRecord{}, ErrExecutionNotFound
		}
		return executionIntentRecord{}, fmt.Errorf("load execution intent: %w", err)
	}
	return current, nil
}

func deleteServiceTruthAfterDeleteExecution(ctx context.Context, tx *sql.Tx, serviceID string, generation int64) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM services
		WHERE id = $1
		  AND generation <= $2
		  AND desired_state = $3
	`, serviceID, generation, cloudmodel.ServiceDesiredDeleted); err != nil {
		return fmt.Errorf("delete service truth after delete execution: %w", err)
	}
	return nil
}

func deleteServiceTruthForCompletedDeleteIntent(ctx context.Context, tx *sql.Tx, serviceID string, generation int64, intentKey string) error {
	var completed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM execution_intents
			WHERE intent_key = $1
			  AND work_action = $2
			  AND status = $3
		)
	`, intentKey, cloudmodel.WorkActionDelete, cloudmodel.StatusSucceeded).Scan(&completed); err != nil {
		return fmt.Errorf("check completed delete service intent: %w", err)
	}
	if !completed {
		return nil
	}
	return deleteServiceTruthAfterDeleteExecution(ctx, tx, serviceID, generation)
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

func replacementDeleteIntentKey(serviceID string, generation int64) string {
	return fmt.Sprintf("%s-stop-before-g%d", serviceID, generation)
}
