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

type serviceRunInput struct {
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

func (in serviceRunInput) validate() error {
	if in.ServiceID == "" {
		return errors.New("serviceID is required")
	}
	if in.ServiceName == "" {
		return errors.New("serviceName is required")
	}
	if in.ServiceGeneration <= 0 {
		return errors.New("serviceGeneration must be greater than 0")
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

func upsertServiceRunTx(ctx context.Context, tx *sql.Tx, input serviceRunInput) error {
	if err := input.validate(); err != nil {
		return err
	}
	commandJSON, err := marshalStringSlice(input.Command)
	if err != nil {
		return fmt.Errorf("marshal service run command: %w", err)
	}
	argsJSON, err := marshalStringSlice(input.Args)
	if err != nil {
		return fmt.Errorf("marshal service run args: %w", err)
	}
	envJSON, err := marshalStringMap(input.Env)
	if err != nil {
		return fmt.Errorf("marshal service run env: %w", err)
	}

	var current serviceRunRecord
	err = tx.QueryRowContext(ctx, `
		SELECT
			id,
			service_generation,
			node_id,
			container_name,
			container_id,
			host_port,
			cpu_milli_request,
			memory_mi_request,
			status
		FROM service_runs
		WHERE service_id = $1
		FOR UPDATE
	`, input.ServiceID).Scan(
		&current.ID,
		&current.ServiceGeneration,
		&current.NodeID,
		&current.ContainerName,
		&current.ContainerID,
		&current.HostPort,
		&current.CPUMilliRequest,
		&current.MemoryMiRequest,
		&current.Status,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load current service run: %w", err)
	}
	if err == nil && current.ServiceGeneration >= input.ServiceGeneration {
		return nil
	}
	if current.NodeID.Valid && current.Status == cloudmodel.StatusRunning {
		if err := freeNodeAllocation(ctx, tx, current.NodeID.String, current.CPUMilliRequest, current.MemoryMiRequest); err != nil {
			return err
		}
	}

	id, err := newID("run")
	if err != nil {
		return err
	}
	if current.ID != "" {
		id = current.ID
	}

	preserveNode := current.NodeID.Valid && current.Status == cloudmodel.StatusRunning
	preserveContainer := preserveNode && current.ContainerID != ""
	var nodeID any
	containerName := ""
	containerID := ""
	hostPort := 0
	startedAt := sql.NullTime{}
	if preserveNode {
		nodeID = current.NodeID.String
	}
	if preserveContainer {
		containerName = current.ContainerName
		containerID = current.ContainerID
		hostPort = current.HostPort
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO service_runs (
			id,
			service_id,
			service_name,
			service_exposure,
			service_generation,
			node_id,
			image,
			command_json,
			args_json,
			env_json,
			container_name,
			container_id,
			container_port,
			host_port,
			readiness_path,
			cpu_milli_request,
			memory_mi_request,
			status,
			status_reason,
			started_at,
			finished_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, NULL)
		ON CONFLICT (service_id) DO UPDATE
		SET
			service_name = EXCLUDED.service_name,
			service_exposure = EXCLUDED.service_exposure,
			service_generation = EXCLUDED.service_generation,
			node_id = EXCLUDED.node_id,
			image = EXCLUDED.image,
			command_json = EXCLUDED.command_json,
			args_json = EXCLUDED.args_json,
			env_json = EXCLUDED.env_json,
			container_name = EXCLUDED.container_name,
			container_id = EXCLUDED.container_id,
			container_port = EXCLUDED.container_port,
			host_port = EXCLUDED.host_port,
			readiness_path = EXCLUDED.readiness_path,
			cpu_milli_request = EXCLUDED.cpu_milli_request,
			memory_mi_request = EXCLUDED.memory_mi_request,
			status = EXCLUDED.status,
			status_reason = EXCLUDED.status_reason,
			started_at = EXCLUDED.started_at,
			finished_at = NULL,
			updated_at = now()
	`,
		id,
		input.ServiceID,
		input.ServiceName,
		input.Exposure,
		input.ServiceGeneration,
		nodeID,
		input.Image,
		commandJSON,
		argsJSON,
		envJSON,
		containerName,
		containerID,
		input.ContainerPort,
		hostPort,
		input.ReadinessPath,
		input.CPUMilliRequest,
		input.MemoryMiRequest,
		cloudmodel.StatusPending,
		"service run accepted",
		startedAt,
	); err != nil {
		return fmt.Errorf("upsert service run: %w", err)
	}
	return nil
}

func deleteServiceRunTx(ctx context.Context, tx *sql.Tx, serviceID string, generation int64) error {
	var run serviceRunRecord
	err := tx.QueryRowContext(ctx, `
		SELECT id, node_id, container_name, container_id, host_port, cpu_milli_request, memory_mi_request, status
		FROM service_runs
		WHERE service_id = $1
		FOR UPDATE
	`, serviceID).Scan(&run.ID, &run.NodeID, &run.ContainerName, &run.ContainerID, &run.HostPort, &run.CPUMilliRequest, &run.MemoryMiRequest, &run.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load service run for delete: %w", err)
	}
	if run.Status == cloudmodel.StatusRunning && run.NodeID.Valid && run.ContainerID != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE service_runs
			SET
				service_generation = $2,
				status = $3,
				status_reason = $4,
				updated_at = now()
			WHERE service_id = $1
		`, serviceID, generation, cloudmodel.StatusPending, "service deletion requested")
		if err != nil {
			return fmt.Errorf("mark service run pending deletion: %w", err)
		}
		return nil
	}
	if run.NodeID.Valid && (run.Status == cloudmodel.StatusDeploying || run.Status == cloudmodel.StatusPending) {
		if err := freeNodeAllocation(ctx, tx, run.NodeID.String, run.CPUMilliRequest, run.MemoryMiRequest); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_runs WHERE service_id = $1`, serviceID); err != nil {
		return fmt.Errorf("delete service run without running container: %w", err)
	}
	return nil
}

func (s *Store) ListIngressRouteSources(ctx context.Context) ([]cloudmodel.RouteSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			services.name,
			services.host,
			r.node_id,
			COALESCE(r.host_port, 0),
			(r.id IS NOT NULL) AS has_backend
		FROM services
		LEFT JOIN service_runs r
			ON r.service_id = services.id
		   AND r.service_generation = services.generation
		   AND r.status = $1
		   AND r.node_id IS NOT NULL
		   AND r.host_port > 0
		WHERE services.desired_state = $2
		  AND services.exposure = $3
		ORDER BY services.name ASC, services.id ASC
	`, cloudmodel.StatusRunning, cloudmodel.ServiceDesiredActive, cloudmodel.ExposurePublic)
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
			service_id,
			service_name,
			service_generation,
			status,
			status_reason,
			updated_at AS observed_at
		FROM service_runs
		ORDER BY updated_at DESC, service_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query service run snapshots: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.ExecutionSnapshot, 0)
	for rows.Next() {
		var item cloudmodel.ExecutionSnapshot
		if err := rows.Scan(
			&item.ServiceID,
			&item.ServiceName,
			&item.ServiceGeneration,
			&item.Status,
			&item.LastStatusReason,
			&item.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan service run snapshot: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service run snapshots: %w", err)
	}
	return items, nil
}

func (s *Store) MarkServiceRunFailed(ctx context.Context, serviceID string, reason string) error {
	if serviceID == "" {
		return errors.New("serviceID is required")
	}
	if reason == "" {
		return errors.New("reason is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE service_runs
		SET
			status = $2,
			status_reason = $3,
			finished_at = COALESCE(finished_at, now()),
			updated_at = now()
		WHERE service_id = $1
		  AND status IN ($4, $5)
	`, serviceID, cloudmodel.StatusFailed, reason, cloudmodel.StatusPending, cloudmodel.StatusDeploying); err != nil {
		return fmt.Errorf("mark service run failed: %w", err)
	}
	return nil
}

func (s *Store) MarkPendingServiceRunFailedForProvisioningNode(ctx context.Context, nodeNamePrefix string, nodeName string, reason string) error {
	if nodeNamePrefix == "" || nodeName == "" {
		return errors.New("name is required")
	}
	if reason == "" {
		return errors.New("reason is required")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE service_runs
		SET
			status = $4,
			status_reason = $5,
			finished_at = COALESCE(finished_at, now()),
			updated_at = now()
		WHERE status = $1
		  AND $2 = $3 || '-' || lower(substr(md5(service_id || '-' || service_generation::text), 1, 10))
	`, cloudmodel.StatusPending, nodeName, nodeNamePrefix, cloudmodel.StatusFailed, reason); err != nil {
		return fmt.Errorf("mark pending service run failed for provisioning node: %w", err)
	}
	return nil
}

func (s *Store) ClaimServiceRun(ctx context.Context, nodeID string) (*cloudmodel.WorkItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin service run claim tx: %w", err)
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
		return nil, fmt.Errorf("load node for service run claim: %w", err)
	}
	if nodeStatus != cloudmodel.StatusReady {
		return nil, nil
	}

	work, cpuMilliRequest, memoryMiRequest, err := claimPendingDeleteRun(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	if work != nil {
		if err := markServiceRunDeploying(ctx, tx, work.ExecutionID, nodeID, work.ContainerName, work.Action); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit delete service run claim tx: %w", err)
		}
		return work, nil
	}
	if !schedulable {
		return nil, nil
	}
	availableCPU := cpuMilliAllocatable - cpuMilliAllocated
	availableMemory := memoryMiAllocatable - memoryMiAllocated
	work, cpuMilliRequest, memoryMiRequest, err = claimPendingRun(ctx, tx, nodeID, availableCPU, availableMemory)
	if err != nil {
		return nil, err
	}
	if work == nil {
		return nil, nil
	}
	work.NodeID = nodeID
	if err := markServiceRunDeploying(ctx, tx, work.ExecutionID, nodeID, work.ContainerName, work.Action); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocated = cpu_milli_allocated + $2,
			memory_mi_allocated = memory_mi_allocated + $3,
			updated_at = now()
		WHERE id = $1
	`, nodeID, cpuMilliRequest, memoryMiRequest); err != nil {
		return nil, fmt.Errorf("reserve node allocation for service run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit service run claim tx: %w", err)
	}
	return work, nil
}

func claimPendingDeleteRun(ctx context.Context, tx *sql.Tx, nodeID string) (*cloudmodel.WorkItem, int, int, error) {
	return loadClaimableServiceRun(ctx, tx, `
		WHERE r.status = $1
		  AND s.desired_state = $2
		  AND r.node_id = $3
		  AND r.container_id <> ''
		ORDER BY r.updated_at ASC, r.id ASC
		LIMIT 1
		FOR UPDATE OF r SKIP LOCKED
	`, cloudmodel.StatusPending, cloudmodel.ServiceDesiredDeleted, nodeID)
}

func claimPendingRun(ctx context.Context, tx *sql.Tx, nodeID string, availableCPU int, availableMemory int) (*cloudmodel.WorkItem, int, int, error) {
	return loadClaimableServiceRun(ctx, tx, `
		WHERE r.status = $1
		  AND s.desired_state = $2
		  AND r.cpu_milli_request <= $3
		  AND r.memory_mi_request <= $4
		  AND (r.node_id IS NULL OR r.node_id = $5)
		ORDER BY r.created_at ASC, r.id ASC
		LIMIT 1
		FOR UPDATE OF r SKIP LOCKED
	`, cloudmodel.StatusPending, cloudmodel.ServiceDesiredActive, availableCPU, availableMemory, nodeID)
}

func loadClaimableServiceRun(ctx context.Context, tx *sql.Tx, whereSQL string, args ...any) (*cloudmodel.WorkItem, int, int, error) {
	query := `
		SELECT
			r.id,
			r.service_id,
			r.service_name,
			r.service_generation,
			r.node_id,
			r.image,
			r.command_json,
			r.args_json,
			r.env_json,
			r.container_port,
			r.readiness_path,
			r.container_name,
			r.container_id,
			r.host_port,
			r.cpu_milli_request,
			r.memory_mi_request,
			s.desired_state
		FROM service_runs r
		JOIN services s ON s.id = r.service_id
		` + whereSQL

	var work cloudmodel.WorkItem
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var serviceGeneration int64
	var storedNodeID sql.NullString
	var desiredState string
	var cpuMilliRequest int
	var memoryMiRequest int
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&work.ExecutionID,
		&work.ServiceID,
		&work.ServiceName,
		&serviceGeneration,
		&storedNodeID,
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
		&desiredState,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, 0, nil
		}
		return nil, 0, 0, fmt.Errorf("load claimable service run: %w", err)
	}
	if storedNodeID.Valid {
		work.NodeID = storedNodeID.String
	}
	if desiredState == cloudmodel.ServiceDesiredDeleted {
		work.Action = cloudmodel.WorkActionDelete
	} else {
		work.Action = cloudmodel.WorkActionRun
	}
	if work.Action == cloudmodel.WorkActionRun {
		work.ContainerName = fmt.Sprintf("mini-cloud-%s-g%d", work.ServiceID, serviceGeneration)
	}
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &work.Command); err != nil {
			return nil, 0, 0, fmt.Errorf("decode service run command: %w", err)
		}
	}
	if work.Command == nil {
		work.Command = []string{}
	}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &work.Args); err != nil {
			return nil, 0, 0, fmt.Errorf("decode service run args: %w", err)
		}
	}
	if work.Args == nil {
		work.Args = []string{}
	}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &work.Env); err != nil {
			return nil, 0, 0, fmt.Errorf("decode service run env: %w", err)
		}
	}
	if work.Env == nil {
		work.Env = map[string]string{}
	}
	return &work, cpuMilliRequest, memoryMiRequest, nil
}

func markServiceRunDeploying(ctx context.Context, tx *sql.Tx, runID string, nodeID string, containerName string, action string) error {
	reason := fmt.Sprintf("agent on node %s claimed service run", nodeID)
	if action == cloudmodel.WorkActionDelete {
		reason = fmt.Sprintf("agent on node %s claimed service delete", nodeID)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE service_runs
		SET
			node_id = $2,
			container_name = $3,
			status = $4,
			status_reason = $5,
			started_at = $6,
			updated_at = now()
		WHERE id = $1
	`, runID, nodeID, containerName, cloudmodel.StatusDeploying, reason, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark service run deploying: %w", err)
	}
	return nil
}

func (s *Store) UpdateExecutionFromNodeReport(ctx context.Context, nodeID string, executionID string, input cloudmodel.ReportInput) (cloudmodel.ReportAck, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.ReportAck{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.ReportAck{}, fmt.Errorf("begin report service run tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := loadServiceRunRecord(ctx, tx, nodeID, executionID)
	if err != nil {
		return cloudmodel.ReportAck{}, err
	}
	if current.Execution.Status != cloudmodel.StatusDeploying {
		if current.Execution.Status == input.Status {
			return cloudmodel.ReportAck{Execution: current.Execution, ObservedAt: time.Now().UTC()}, nil
		}
		return cloudmodel.ReportAck{}, fmt.Errorf("service run %s is already %s and cannot transition to %s", executionID, current.Execution.Status, input.Status)
	}

	observedAt := time.Now().UTC()
	var finishedAt sql.NullTime
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSucceeded {
		finishedAt = sql.NullTime{Time: observedAt, Valid: true}
	}
	updated, err := updateServiceRunReport(ctx, tx, executionID, input, finishedAt)
	if err != nil {
		return cloudmodel.ReportAck{}, err
	}
	if input.Status == cloudmodel.StatusFailed || input.Status == cloudmodel.StatusSucceeded {
		if err := freeNodeAllocation(ctx, tx, nodeID, current.CPUMilliRequest, current.MemoryMiRequest); err != nil {
			return cloudmodel.ReportAck{}, err
		}
	}
	if input.Status == cloudmodel.StatusSucceeded && current.DesiredState == cloudmodel.ServiceDesiredDeleted {
		if _, err := tx.ExecContext(ctx, `DELETE FROM service_runs WHERE id = $1`, executionID); err != nil {
			return cloudmodel.ReportAck{}, fmt.Errorf("delete completed service run: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return cloudmodel.ReportAck{}, fmt.Errorf("commit report service run tx: %w", err)
	}
	return cloudmodel.ReportAck{Execution: updated, ObservedAt: observedAt}, nil
}

func updateServiceRunReport(ctx context.Context, tx *sql.Tx, executionID string, input cloudmodel.ReportInput, finishedAt sql.NullTime) (cloudmodel.ExecutionRecord, error) {
	var updated cloudmodel.ExecutionRecord
	err := tx.QueryRowContext(ctx, `
		UPDATE service_runs
		SET
			container_name = $2,
			container_id = $3,
			host_port = $4,
			status = $5,
			status_reason = $6,
			finished_at = $7,
			updated_at = now()
		WHERE id = $1
		RETURNING id, node_id, image, container_name, container_id, container_port, host_port, readiness_path, status, status_reason, started_at, finished_at, created_at, updated_at
	`, executionID, input.ContainerName, input.ContainerID, input.HostPort, input.Status, input.Reason, finishedAt).Scan(
		&updated.ID,
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
		return cloudmodel.ExecutionRecord{}, fmt.Errorf("update service run report: %w", err)
	}
	return updated, nil
}

type serviceRunRecord struct {
	Execution         cloudmodel.ExecutionRecord
	ServiceGeneration int64
	DesiredState      string
	NodeID            sql.NullString
	ContainerName     string
	ContainerID       string
	HostPort          int
	Status            string
	CPUMilliRequest   int
	MemoryMiRequest   int
	ID                string
}

func loadServiceRunRecord(ctx context.Context, tx *sql.Tx, nodeID string, executionID string) (serviceRunRecord, error) {
	var current serviceRunRecord
	err := tx.QueryRowContext(ctx, `
		SELECT
			r.id,
			r.service_generation,
			s.desired_state,
			r.node_id,
			r.image,
			r.container_name,
			r.container_id,
			r.container_port,
			r.host_port,
			r.readiness_path,
			r.status,
			r.status_reason,
			r.started_at,
			r.finished_at,
			r.created_at,
			r.updated_at,
			r.cpu_milli_request,
			r.memory_mi_request
		FROM service_runs r
		JOIN services s ON s.id = r.service_id
		WHERE r.id = $1
		  AND r.node_id = $2
		FOR UPDATE OF r
	`, executionID, nodeID).Scan(
		&current.Execution.ID,
		&current.ServiceGeneration,
		&current.DesiredState,
		&current.NodeID,
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
			return serviceRunRecord{}, ErrExecutionNotFound
		}
		return serviceRunRecord{}, fmt.Errorf("load service run: %w", err)
	}
	if current.NodeID.Valid {
		current.Execution.NodeID = current.NodeID.String
	}
	current.ID = current.Execution.ID
	current.ContainerName = current.Execution.ContainerName
	current.ContainerID = current.Execution.ContainerID
	current.HostPort = current.Execution.HostPort
	current.Status = current.Execution.Status
	return current, nil
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
		return fmt.Errorf("free node allocation for service run: %w", err)
	}
	return nil
}

func marshalStringSlice(input []string) ([]byte, error) {
	if input == nil {
		input = []string{}
	}
	return json.Marshal(input)
}

func marshalStringMap(input map[string]string) ([]byte, error) {
	if input == nil {
		input = map[string]string{}
	}
	return json.Marshal(input)
}
