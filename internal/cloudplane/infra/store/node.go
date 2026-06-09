package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNodeNotFound                         = errors.New("node not found")
	ErrHeartbeatCPUMilliAllocatableTooLarge = errors.New("heartbeat cpuMilliAllocatable exceeds node cpuMilliTotal")
	ErrHeartbeatMemoryMiAllocatableTooLarge = errors.New("heartbeat memoryMiAllocatable exceeds node memoryMiTotal")
	ErrNodeProviderInstanceAlreadyExists    = errors.New("node with this provider and instanceID already exists")
)

const nodeSelectColumns = `
	id,
	provider,
	region,
	name,
	private_ip,
	public_ip,
	instance_id,
	instance_type,
	cpu_milli_total,
	memory_mi_total,
	cpu_milli_allocatable,
	memory_mi_allocatable,
	cpu_milli_allocated,
	memory_mi_allocated,
	status,
	status_reason,
	schedulable,
	last_heartbeat_at,
	created_at,
	updated_at
`

func (s *Store) CreateProvisioningNode(ctx context.Context, input cloudmodel.ProvisioningInput) (cloudmodel.Node, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.Node{}, err
	}
	id, err := newID("node")
	if err != nil {
		return cloudmodel.Node{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO nodes (
			id,
			provider,
			region,
			name,
			instance_type,
			status,
			status_reason,
			schedulable
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE)
		RETURNING `+nodeSelectColumns+`
	`, id, input.Provider, input.Region, input.Name, input.InstanceType, cloudmodel.StatusProvisioning, input.StatusReason)

	item, err := scanNode(row)
	if err != nil {
		return cloudmodel.Node{}, fmt.Errorf("create provisioning node: %w", err)
	}
	return item, nil
}

func (s *Store) BindProvisionedNode(ctx context.Context, nodeID string, instanceID string, instanceName string, instanceType string, reason string, observedAt time.Time) (cloudmodel.Node, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			instance_id = NULLIF($2, ''),
			name = $3,
			instance_type = $4,
			status_reason = $5,
			updated_at = $6
		WHERE id = $1
		  AND status = $7
		RETURNING `+nodeSelectColumns+`
	`, nodeID, strings.TrimSpace(instanceID), strings.TrimSpace(instanceName), strings.TrimSpace(instanceType), reason, observedAt.UTC(), cloudmodel.StatusProvisioning)

	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, ErrNodeNotFound
		}
		return cloudmodel.Node{}, fmt.Errorf("bind provisioned node: %w", err)
	}
	return item, nil
}

func (s *Store) RegisterNode(ctx context.Context, input cloudmodel.RegisterInput) (cloudmodel.Node, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.Node{}, err
	}

	id, err := newID("node")
	if err != nil {
		return cloudmodel.Node{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO nodes (
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			status_reason,
			schedulable
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, 0, 0, 0, 0, $11, '', TRUE)
		ON CONFLICT (provider, instance_id) DO UPDATE
		SET
			region = EXCLUDED.region,
			name = EXCLUDED.name,
			private_ip = EXCLUDED.private_ip,
			public_ip = EXCLUDED.public_ip,
			instance_type = EXCLUDED.instance_type,
			cpu_milli_total = EXCLUDED.cpu_milli_total,
			memory_mi_total = EXCLUDED.memory_mi_total,
			status = CASE
				WHEN nodes.status IN ($12, $13) THEN nodes.status
				ELSE $11
			END,
			status_reason = CASE
				WHEN nodes.status IN ($12, $13) THEN nodes.status_reason
				ELSE ''
			END,
			schedulable = CASE
				WHEN nodes.status IN ($12, $13) THEN FALSE
				ELSE TRUE
			END,
			updated_at = now()
		RETURNING `+nodeSelectColumns+`
	`,
		id,
		input.Provider,
		input.Region,
		input.Name,
		input.PrivateIP,
		input.PublicIP,
		input.InstanceID,
		input.InstanceType,
		input.CPUMilliTotal,
		input.MemoryMiTotal,
		cloudmodel.StatusRegistering,
		cloudmodel.StatusDraining,
		cloudmodel.StatusDeleted,
	)

	registered, err := scanNode(row)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return cloudmodel.Node{}, ErrNodeProviderInstanceAlreadyExists
		}
		return cloudmodel.Node{}, fmt.Errorf("register node: %w", err)
	}
	return registered, nil
}

func (s *Store) ListNodes(ctx context.Context) ([]cloudmodel.Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer closeRows(rows)

	items := make([]cloudmodel.Node, 0)
	for rows.Next() {
		item, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return items, nil
}

func (s *Store) ListNodesByStatuses(ctx context.Context, statuses ...string) ([]cloudmodel.Node, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE status = ANY($1::text[])
		ORDER BY created_at ASC, id ASC
	`, statuses)
	if err != nil {
		return nil, fmt.Errorf("query nodes by statuses: %w", err)
	}
	defer closeRows(rows)

	items := make([]cloudmodel.Node, 0)
	for rows.Next() {
		item, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node by statuses: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes by statuses: %w", err)
	}
	return items, nil
}

func (s *Store) RecordNodeHeartbeat(ctx context.Context, nodeID string, input cloudmodel.HeartbeatInput) (cloudmodel.HeartbeatSummary, time.Time, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, fmt.Errorf("begin heartbeat tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cpuTotal int
	var memoryTotal int
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT cpu_milli_total, memory_mi_total, status
		FROM nodes
		WHERE id = $1
	`, nodeID).Scan(&cpuTotal, &memoryTotal, &currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.HeartbeatSummary{}, time.Time{}, ErrNodeNotFound
		}
		return cloudmodel.HeartbeatSummary{}, time.Time{}, fmt.Errorf("load node capacity: %w", err)
	}

	if input.CPUMilliAllocatable > cpuTotal {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, ErrHeartbeatCPUMilliAllocatableTooLarge
	}
	if input.MemoryMiAllocatable > memoryTotal {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, ErrHeartbeatMemoryMiAllocatableTooLarge
	}

	reportedAt := input.ReportedAt.UTC()
	receivedAt := time.Now().UTC()
	nextStatus := input.Status
	nextSchedulable := input.Status == cloudmodel.StatusReady
	switch currentStatus {
	case cloudmodel.StatusDraining:
		nextStatus = cloudmodel.StatusDraining
		nextSchedulable = false
	case cloudmodel.StatusDeleted:
		nextStatus = cloudmodel.StatusDeleted
		nextSchedulable = false
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocatable = $2,
			memory_mi_allocatable = $3,
			status = $4,
			status_reason = '',
			schedulable = $5,
			last_heartbeat_at = $6,
			updated_at = CASE
				WHEN cpu_milli_allocatable IS DISTINCT FROM $2
					OR memory_mi_allocatable IS DISTINCT FROM $3
					OR status IS DISTINCT FROM $4
					OR schedulable IS DISTINCT FROM $5
				THEN now()
				ELSE updated_at
			END
		WHERE id = $1
	`, nodeID, input.CPUMilliAllocatable, input.MemoryMiAllocatable, nextStatus, nextSchedulable, receivedAt); err != nil {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, fmt.Errorf("update node summary from heartbeat: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return cloudmodel.HeartbeatSummary{}, time.Time{}, fmt.Errorf("commit heartbeat tx: %w", err)
	}
	return cloudmodel.HeartbeatSummary{
		ReportedAt:          reportedAt,
		AgentVersion:        input.AgentVersion,
		CPUMilliAllocatable: input.CPUMilliAllocatable,
		MemoryMiAllocatable: input.MemoryMiAllocatable,
		RunningContainers:   input.RunningContainers,
		Status:              nextStatus,
	}, receivedAt, nil
}

func (s *Store) GetNode(ctx context.Context, nodeID string) (cloudmodel.Node, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE id = $1
	`, nodeID)

	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, ErrNodeNotFound
		}
		return cloudmodel.Node{}, fmt.Errorf("query node: %w", err)
	}
	return item, nil
}

func scanNode(scanner interface{ Scan(dest ...any) error }) (cloudmodel.Node, error) {
	var item cloudmodel.Node
	var instanceID sql.NullString
	var lastHeartbeatAt sql.NullTime

	err := scanner.Scan(
		&item.ID,
		&item.Provider,
		&item.Region,
		&item.Name,
		&item.PrivateIP,
		&item.PublicIP,
		&instanceID,
		&item.InstanceType,
		&item.CPUMilliTotal,
		&item.MemoryMiTotal,
		&item.CPUMilliAllocatable,
		&item.MemoryMiAllocatable,
		&item.CPUMilliAllocated,
		&item.MemoryMiAllocated,
		&item.Status,
		&item.StatusReason,
		&item.Schedulable,
		&lastHeartbeatAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return cloudmodel.Node{}, err
	}
	if instanceID.Valid {
		item.InstanceID = instanceID.String
	}
	if lastHeartbeatAt.Valid {
		item.LastHeartbeatAt = &lastHeartbeatAt.Time
	}
	return item, nil
}
