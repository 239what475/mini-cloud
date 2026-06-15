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
	elastic,
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
			schedulable,
			elastic
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, TRUE)
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
			elastic
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, 0, 0, 0, 0, $10, '', TRUE, FALSE)
		ON CONFLICT (provider, instance_id) DO UPDATE
		SET
			region = EXCLUDED.region,
			name = EXCLUDED.name,
			private_ip = EXCLUDED.private_ip,
			instance_type = EXCLUDED.instance_type,
			cpu_milli_total = EXCLUDED.cpu_milli_total,
			memory_mi_total = EXCLUDED.memory_mi_total,
			status = CASE
				WHEN nodes.status IN ($11, $12) THEN nodes.status
				ELSE $10
			END,
			status_reason = CASE
				WHEN nodes.status IN ($11, $12) THEN nodes.status_reason
				ELSE ''
			END,
			schedulable = CASE
				WHEN nodes.status IN ($11, $12) THEN FALSE
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
	defer func() { _ = rows.Close() }()

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

func (s *Store) ListElasticNodesByStatuses(ctx context.Context, statuses ...string) ([]cloudmodel.Node, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE status = ANY($1::text[])
		  AND elastic
		ORDER BY created_at ASC, id ASC
	`, statuses)
	if err != nil {
		return nil, fmt.Errorf("query elastic nodes by statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]cloudmodel.Node, 0)
	for rows.Next() {
		item, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan elastic node by statuses: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate elastic nodes by statuses: %w", err)
	}
	return items, nil
}

func (s *Store) RecordNodeHeartbeat(ctx context.Context, nodeID string, input cloudmodel.HeartbeatInput) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin heartbeat tx: %w", err)
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
			return "", ErrNodeNotFound
		}
		return "", fmt.Errorf("load node capacity: %w", err)
	}

	if input.CPUMilliAllocatable > cpuTotal {
		return "", ErrHeartbeatCPUMilliAllocatableTooLarge
	}
	if input.MemoryMiAllocatable > memoryTotal {
		return "", ErrHeartbeatMemoryMiAllocatableTooLarge
	}

	receivedAt := time.Now().UTC()
	nextStatus := cloudmodel.StatusReady
	nextSchedulable := true
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
		return "", fmt.Errorf("update node summary from heartbeat: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit heartbeat tx: %w", err)
	}
	return nextStatus, nil
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

type NodeProvisioningCandidate struct {
	ServiceID       string
	CPUMilli        int
	MemoryMi        int
	NodeName        string
	InstanceType    string
	ClientToken     string
	HasCapacity     bool
	HasProvisioning bool
}

func (s *Store) GetNodeProvisioningCandidate(ctx context.Context, nodeNamePrefix string, instanceType string) (*NodeProvisioningCandidate, error) {
	var candidate NodeProvisioningCandidate
	err := s.db.QueryRowContext(ctx, `
		WITH pending AS (
			SELECT
				service_id,
				service_generation,
				cpu_milli_request,
				memory_mi_request,
				created_at
			FROM service_runs
			WHERE status = $1
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		),
		capacity AS (
			SELECT EXISTS (
				SELECT 1
				FROM nodes, pending
				WHERE status = $2
				  AND schedulable
				  AND cpu_milli_allocatable - cpu_milli_allocated >= pending.cpu_milli_request
				  AND memory_mi_allocatable - memory_mi_allocated >= pending.memory_mi_request
			) AS has_capacity
		),
		provisioning AS (
			SELECT EXISTS (
				SELECT 1
				FROM nodes, pending
				WHERE status = $3
				  AND name = $4 || '-' || lower(substr(md5(pending.service_id || '-' || pending.service_generation::text), 1, 10))
				  AND instance_type = $5
			) AS has_provisioning
		)
		SELECT
			pending.service_id,
			pending.cpu_milli_request,
			pending.memory_mi_request,
			$4 || '-' || lower(substr(md5(pending.service_id || '-' || pending.service_generation::text), 1, 10)),
			$5,
			pending.service_id || '-' || pending.service_generation::text,
			capacity.has_capacity,
			provisioning.has_provisioning
		FROM pending
		CROSS JOIN capacity
		CROSS JOIN provisioning
	`, cloudmodel.StatusPending, cloudmodel.StatusReady, cloudmodel.StatusProvisioning, nodeNamePrefix, instanceType).Scan(
		&candidate.ServiceID,
		&candidate.CPUMilli,
		&candidate.MemoryMi,
		&candidate.NodeName,
		&candidate.InstanceType,
		&candidate.ClientToken,
		&candidate.HasCapacity,
		&candidate.HasProvisioning,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query node provisioning candidate: %w", err)
	}
	return &candidate, nil
}

func (s *Store) HasUnsettledServiceRuns(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_runs
			WHERE status IN ($1, $2)
		)
	`, cloudmodel.StatusPending, cloudmodel.StatusDeploying).Scan(&exists); err != nil {
		return false, fmt.Errorf("check unsettled service runs: %w", err)
	}
	return exists, nil
}

func (s *Store) PrepareNodeDeletion(ctx context.Context, nodeID string, reason string, observedAt time.Time) (cloudmodel.Node, bool, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("begin node deletion tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	currentRow := tx.QueryRowContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE id = $1
		FOR UPDATE
	`, nodeID)
	current, err := scanNode(currentRow)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, false, ErrNodeNotFound
		}
		return cloudmodel.Node{}, false, fmt.Errorf("load node for deletion: %w", err)
	}
	if current.Status == cloudmodel.StatusDraining {
		return current, true, nil
	}
	if current.Status != cloudmodel.StatusReady || !current.Elastic || strings.TrimSpace(current.InstanceID) == "" {
		return current, false, nil
	}

	var activeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM service_runs
		WHERE node_id = $1
		  AND status IN ($2, $3)
	`, current.ID, cloudmodel.StatusDeploying, cloudmodel.StatusRunning).Scan(&activeCount); err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("count active service runs before node deletion: %w", err)
	}
	if activeCount > 0 {
		return current, false, nil
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			status = $2,
			status_reason = $3,
			schedulable = FALSE,
			updated_at = $4
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, nodeID, cloudmodel.StatusDraining, reason, observedAt.UTC())
	item, err := scanNode(row)
	if err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("prepare node deletion: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return cloudmodel.Node{}, false, fmt.Errorf("commit node deletion tx: %w", err)
	}
	return item, true, nil
}

func (s *Store) MarkNodeDeleted(ctx context.Context, nodeID string, reason string, observedAt time.Time) (cloudmodel.Node, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			private_ip = '',
			instance_id = NULL,
			status = $2,
			status_reason = $3,
			schedulable = FALSE,
			cpu_milli_allocated = 0,
			memory_mi_allocated = 0,
			updated_at = $4
		WHERE id = $1
		RETURNING `+nodeSelectColumns+`
	`, nodeID, cloudmodel.StatusDeleted, reason, observedAt.UTC())
	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Node{}, ErrNodeNotFound
		}
		return cloudmodel.Node{}, fmt.Errorf("mark node deleted: %w", err)
	}
	return item, nil
}

var ErrHeartbeatStaleAfterInvalid = errors.New("staleAfter must be greater than 0")

type StaleNodeHeartbeatResult struct {
	OfflineNodes     int
	FailedExecutions int
}

func (s *Store) MarkStaleNodeHeartbeatsOffline(ctx context.Context, staleAfter time.Duration) (StaleNodeHeartbeatResult, error) {
	if staleAfter <= 0 {
		return StaleNodeHeartbeatResult{}, ErrHeartbeatStaleAfterInvalid
	}

	cutoffTime := time.Now().UTC().Add(-staleAfter)
	var result StaleNodeHeartbeatResult

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StaleNodeHeartbeatResult{}, fmt.Errorf("begin reconcile stale heartbeats tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT `+nodeSelectColumns+`
		FROM nodes
		WHERE last_heartbeat_at IS NOT NULL
		  AND last_heartbeat_at < $1
		  AND status <> $2
		  AND status <> $3
		  AND status <> $4
		ORDER BY last_heartbeat_at ASC, id ASC
		FOR UPDATE
	`, cutoffTime, cloudmodel.StatusOffline, cloudmodel.StatusDraining, cloudmodel.StatusDeleted)
	if err != nil {
		return StaleNodeHeartbeatResult{}, fmt.Errorf("query stale nodes: %w", err)
	}

	var staleNodes []cloudmodel.Node
	for rows.Next() {
		item, scanErr := scanNode(rows)
		if scanErr != nil {
			_ = rows.Close()
			return StaleNodeHeartbeatResult{}, fmt.Errorf("scan stale node: %w", scanErr)
		}
		staleNodes = append(staleNodes, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return StaleNodeHeartbeatResult{}, fmt.Errorf("iterate stale nodes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return StaleNodeHeartbeatResult{}, fmt.Errorf("close stale node rows: %w", err)
	}

	for _, staleNode := range staleNodes {
		if _, err := tx.ExecContext(ctx, `
			UPDATE nodes
			SET
				status = $2,
				status_reason = $3,
				schedulable = FALSE,
				updated_at = now()
			WHERE id = $1
		`, staleNode.ID, cloudmodel.StatusOffline, "node heartbeat timed out"); err != nil {
			return StaleNodeHeartbeatResult{}, fmt.Errorf("mark stale node offline: %w", err)
		}
		result.OfflineNodes++

		reason := fmt.Sprintf(
			"node %s marked offline because no heartbeat arrived after %s",
			staleNode.Name,
			cutoffTime.Format(time.RFC3339),
		)

		failedExecutions, err := failServiceRunsForOfflineNode(ctx, tx, staleNode, reason)
		if err != nil {
			return StaleNodeHeartbeatResult{}, err
		}
		result.FailedExecutions += failedExecutions
	}

	if err := tx.Commit(); err != nil {
		return StaleNodeHeartbeatResult{}, fmt.Errorf("commit reconcile stale heartbeats tx: %w", err)
	}
	return result, nil
}

type impactedServiceRun struct {
	CPUMilliRequest int
	MemoryMiRequest int
	ID              string
}

func failServiceRunsForOfflineNode(ctx context.Context, tx *sql.Tx, staleNode cloudmodel.Node, reason string) (int, error) {
	runRows, err := tx.QueryContext(ctx, `
		SELECT
			id,
			cpu_milli_request,
			memory_mi_request
		FROM service_runs
		WHERE node_id = $1
		  AND status IN ($2, $3, $4)
		ORDER BY updated_at ASC, id ASC
		FOR UPDATE
	`, staleNode.ID, cloudmodel.StatusPending, cloudmodel.StatusDeploying, cloudmodel.StatusRunning)
	if err != nil {
		return 0, fmt.Errorf("query impacted service runs: %w", err)
	}

	var impacted []impactedServiceRun
	for runRows.Next() {
		var item impactedServiceRun
		if err := runRows.Scan(&item.ID, &item.CPUMilliRequest, &item.MemoryMiRequest); err != nil {
			_ = runRows.Close()
			return 0, fmt.Errorf("scan impacted service run: %w", err)
		}
		impacted = append(impacted, item)
	}
	if err := runRows.Err(); err != nil {
		_ = runRows.Close()
		return 0, fmt.Errorf("iterate impacted service runs: %w", err)
	}
	if err := runRows.Close(); err != nil {
		return 0, fmt.Errorf("close impacted service run rows: %w", err)
	}

	for _, item := range impacted {
		if _, err := tx.ExecContext(ctx, `
			UPDATE service_runs
			SET
				status = $2,
				status_reason = $3,
				finished_at = COALESCE(finished_at, now()),
				updated_at = now()
			WHERE id = $1
		`, item.ID, cloudmodel.StatusFailed, reason); err != nil {
			return 0, fmt.Errorf("mark service run failed during node offline reconcile: %w", err)
		}
		if err := freeNodeAllocation(ctx, tx, staleNode.ID, item.CPUMilliRequest, item.MemoryMiRequest); err != nil {
			return 0, err
		}
	}
	return len(impacted), nil
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
		&item.Elastic,
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
