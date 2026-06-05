package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	plane "mini-cloud/internal/controlplane/plane"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrPlaneNotFound                = errors.New("plane not found")
	ErrPlaneNameAlreadyExists       = errors.New("plane name already exists")
	ErrPlaneSouthboundTokenNotFound = errors.New("plane southbound token not found")
	defaultPlaneStatusMessage       = "awaiting registration handshake"
	defaultPlaneOperationReason     = ""
)

func (s *Store) CreatePlane(ctx context.Context, input plane.CreateInput) (plane.Detail, error) {
	if err := input.Validate(); err != nil {
		return plane.Detail{}, err
	}

	id, err := newID("pln")
	if err != nil {
		return plane.Detail{}, err
	}

	grpcEndpoint, err := input.ResolvedGRPCEndpoint()
	if err != nil {
		return plane.Detail{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return plane.Detail{}, fmt.Errorf("begin create plane tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var created plane.Plane
	err = tx.QueryRowContext(ctx, `
		INSERT INTO fleet_planes (
			id,
			name,
			display_name,
			provider,
			region,
			grpc_endpoint
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			name,
			display_name,
			provider,
			region,
			grpc_endpoint,
			created_at
	`, id, input.Name, input.DisplayName, input.Provider, input.Region, grpcEndpoint).Scan(
		&created.ID,
		&created.Name,
		&created.DisplayName,
		&created.Provider,
		&created.Region,
		&created.GRPCEndpoint,
		&created.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return plane.Detail{}, ErrPlaneNameAlreadyExists
		}
		return plane.Detail{}, fmt.Errorf("insert plane: %w", err)
	}

	var status plane.PlaneStatus
	err = tx.QueryRowContext(ctx, `
		INSERT INTO fleet_plane_statuses (
			plane_id,
			status,
			message
		)
		VALUES ($1, $2, $3)
		RETURNING
			plane_id,
			status,
			message,
			last_heartbeat_at,
			last_sync_at,
			updated_at
	`, created.ID, plane.StatusRegistering, defaultPlaneStatusMessage).Scan(
		&status.PlaneID,
		&status.Status,
		&status.Message,
		new(sql.NullTime),
		new(sql.NullTime),
		&status.UpdatedAt,
	)
	if err != nil {
		return plane.Detail{}, fmt.Errorf("insert plane status: %w", err)
	}
	status.Status = plane.StatusRegistering
	status.Message = defaultPlaneStatusMessage

	var operation plane.Operation
	err = tx.QueryRowContext(ctx, `
		INSERT INTO fleet_plane_operations (
			plane_id,
			state,
			reason
		)
		VALUES ($1, $2, $3)
		RETURNING
			plane_id,
			state,
			reason,
			updated_at
	`, created.ID, plane.OperationStateActive, defaultPlaneOperationReason).Scan(
		&operation.PlaneID,
		&operation.State,
		&operation.Reason,
		&operation.UpdatedAt,
	)
	if err != nil {
		return plane.Detail{}, fmt.Errorf("insert plane operation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return plane.Detail{}, fmt.Errorf("commit create plane: %w", err)
	}

	return plane.Detail{
		Plane:     created,
		Status:    status,
		Operation: operation,
	}, nil
}

func (s *Store) ListPlanes(ctx context.Context) ([]plane.Detail, error) {
	rows, err := s.db.QueryContext(ctx, planeDetailBaseQuery(`
		ORDER BY p.created_at ASC, p.id ASC
	`))
	if err != nil {
		return nil, fmt.Errorf("query control planes: %w", err)
	}
	defer closeRows(rows)

	items := make([]plane.Detail, 0)
	for rows.Next() {
		item, err := scanPlaneDetail(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control planes: %w", err)
	}
	return items, nil
}

func (s *Store) GetPlane(ctx context.Context, planeID string) (plane.Detail, error) {
	row := s.db.QueryRowContext(ctx, planeDetailBaseQuery(`
		WHERE p.id = $1
	`), planeID)

	item, err := scanPlaneDetail(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return plane.Detail{}, ErrPlaneNotFound
		}
		return plane.Detail{}, fmt.Errorf("query plane: %w", err)
	}
	return item, nil
}

func (s *Store) DeletePlane(ctx context.Context, planeID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_planes WHERE id = $1`, planeID)
	if err != nil {
		return fmt.Errorf("delete plane: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrPlaneNotFound
	}
	return nil
}

func (s *Store) SetPlaneSouthboundToken(ctx context.Context, planeID string, southboundToken string) (plane.Registration, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO plane_southbound_tokens (
			plane_id,
			southbound_token
		)
		VALUES ($1, $2)
		ON CONFLICT (plane_id)
		DO UPDATE SET
			southbound_token = EXCLUDED.southbound_token,
			updated_at = now()
		RETURNING
			last_verified_at,
			updated_at
	`, planeID, southboundToken)

	registration, err := scanPlaneRegistration(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return plane.Registration{}, ErrPlaneNotFound
		}
		return plane.Registration{}, fmt.Errorf("set plane southbound token: %w", err)
	}
	registration.Registered = true
	return registration, nil
}

func (s *Store) MarkPlaneSouthboundTokenVerified(ctx context.Context, planeID string, verifiedAt time.Time) (plane.Registration, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE plane_southbound_tokens
		SET
			last_verified_at = $2
		WHERE plane_id = $1
		RETURNING
			last_verified_at,
			updated_at
	`, planeID, verifiedAt.UTC())

	registration, err := scanPlaneRegistration(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return plane.Registration{}, ErrPlaneSouthboundTokenNotFound
		}
		return plane.Registration{}, fmt.Errorf("mark plane southbound token verified: %w", err)
	}
	registration.Registered = true
	return registration, nil
}

func (s *Store) GetPlaneSouthboundToken(ctx context.Context, planeID string) (string, error) {
	var token string
	err := s.db.QueryRowContext(ctx, `
		SELECT southbound_token
		FROM plane_southbound_tokens
		WHERE plane_id = $1
	`, planeID).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrPlaneSouthboundTokenNotFound
		}
		return "", fmt.Errorf("get plane southbound token: %w", err)
	}
	return token, nil
}

func (s *Store) ListRegisteredPlaneIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plane_id
		FROM plane_southbound_tokens
		ORDER BY created_at ASC, plane_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query registered plane ids: %w", err)
	}
	defer closeRows(rows)

	items := make([]string, 0)
	for rows.Next() {
		var planeID string
		if err := rows.Scan(&planeID); err != nil {
			return nil, fmt.Errorf("scan registered plane id: %w", err)
		}
		items = append(items, planeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registered plane ids: %w", err)
	}
	return items, nil
}

func (s *Store) UpdatePlaneStatus(ctx context.Context, planeID string, input plane.UpdateStatusInput) (plane.PlaneStatus, error) {
	if err := input.Validate(); err != nil {
		return plane.PlaneStatus{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		UPDATE fleet_plane_statuses
		SET
			status = $2,
			message = $3,
			last_heartbeat_at = $4,
			last_sync_at = $5,
			updated_at = now()
		WHERE plane_id = $1
		RETURNING
			plane_id,
			status,
			message,
			last_heartbeat_at,
			last_sync_at,
			updated_at
	`, planeID, input.Status, input.Message, nullableTime(input.LastHeartbeatAt), nullableTime(input.LastSyncAt))

	status, err := scanPlaneStatus(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return plane.PlaneStatus{}, ErrPlaneNotFound
		}
		return plane.PlaneStatus{}, fmt.Errorf("update plane status: %w", err)
	}
	return status, nil
}

func (s *Store) UpdatePlaneOperation(ctx context.Context, planeID string, input plane.UpdateOperationInput) (plane.Operation, error) {
	if err := input.Validate(); err != nil {
		return plane.Operation{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		UPDATE fleet_plane_operations
		SET
			state = $2,
			reason = $3,
			updated_at = now()
		WHERE plane_id = $1
		RETURNING
			plane_id,
			state,
			reason,
			updated_at
	`, planeID, input.State, input.ResolvedReason())

	operation, err := scanPlaneOperation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return plane.Operation{}, ErrPlaneNotFound
		}
		return plane.Operation{}, fmt.Errorf("update plane operation: %w", err)
	}
	return operation, nil
}

func (s *Store) RecordPlaneCapacitySnapshot(ctx context.Context, planeID string, input plane.RecordCapacitySnapshotInput) (plane.CapacitySnapshot, error) {
	if err := input.Validate(); err != nil {
		return plane.CapacitySnapshot{}, err
	}

	id, err := newID("pcs")
	if err != nil {
		return plane.CapacitySnapshot{}, err
	}

	capturedAt := input.ResolvedCapturedAt(time.Now())
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO fleet_plane_capacity_snapshots (
			id,
			plane_id,
			nodes_total,
			nodes_ready,
			services_total,
			runs_total,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			captured_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING
			id,
			plane_id,
			nodes_total,
			nodes_ready,
			services_total,
			runs_total,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			captured_at
	`, id, planeID, input.NodesTotal, input.NodesReady, input.ServicesTotal, input.RunsTotal, input.CPUMilliCapacity, input.CPUMilliAllocated, input.MemoryMiCapacity, input.MemoryMiAllocated, capturedAt)

	item, err := scanPlaneCapacitySnapshot(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return plane.CapacitySnapshot{}, ErrPlaneNotFound
		}
		return plane.CapacitySnapshot{}, fmt.Errorf("insert plane capacity snapshot: %w", err)
	}
	return item, nil
}

func (s *Store) ListPlaneCapacitySnapshots(ctx context.Context, planeID string, limit int) ([]plane.CapacitySnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			plane_id,
			nodes_total,
			nodes_ready,
			services_total,
			runs_total,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			captured_at
		FROM fleet_plane_capacity_snapshots
		WHERE plane_id = $1
		ORDER BY captured_at DESC, id DESC
		LIMIT $2
	`, planeID, limit)
	if err != nil {
		return nil, fmt.Errorf("query plane capacity snapshots: %w", err)
	}
	defer closeRows(rows)

	items := make([]plane.CapacitySnapshot, 0)
	for rows.Next() {
		item, err := scanPlaneCapacitySnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plane capacity snapshots: %w", err)
	}
	return items, nil
}

func planeDetailBaseQuery(suffix string) string {
	return `
		SELECT
			p.id,
			p.name,
			p.display_name,
			p.provider,
			p.region,
			p.grpc_endpoint,
			p.created_at,
			s.plane_id,
			s.status,
			s.message,
			s.last_heartbeat_at,
			s.last_sync_at,
			s.updated_at,
			bt.last_verified_at,
			bt.updated_at,
			o.plane_id,
			o.state,
			o.reason,
			o.updated_at,
			cs.id,
			cs.plane_id,
			cs.nodes_total,
			cs.nodes_ready,
			cs.services_total,
			cs.runs_total,
			cs.cpu_milli_capacity,
			cs.cpu_milli_allocated,
			cs.memory_mi_capacity,
			cs.memory_mi_allocated,
			cs.captured_at,
			ris.plane_id,
			ris.sync_version,
			ris.observed_at,
			ris.nodes_total,
			ris.nodes_ready,
			ris.cpu_milli_capacity,
			ris.cpu_milli_allocated,
			ris.memory_mi_capacity,
			ris.memory_mi_allocated,
			ris.updated_at,
			rcs.plane_id,
			rcs.observed_at,
			rcs.fingerprint,
			rcs.summary_json,
			rcs.updated_at
		FROM fleet_planes p
		JOIN fleet_plane_statuses s
			ON s.plane_id = p.id
		LEFT JOIN plane_southbound_tokens bt
			ON bt.plane_id = p.id
		JOIN fleet_plane_operations o
			ON o.plane_id = p.id
		LEFT JOIN LATERAL (
			SELECT
				id,
				plane_id,
				nodes_total,
				nodes_ready,
				services_total,
				runs_total,
				cpu_milli_capacity,
				cpu_milli_allocated,
				memory_mi_capacity,
				memory_mi_allocated,
				captured_at
			FROM fleet_plane_capacity_snapshots
			WHERE plane_id = p.id
			ORDER BY captured_at DESC, id DESC
			LIMIT 1
		) cs ON true
		LEFT JOIN fleet_plane_runtime_inventory_states ris
			ON ris.plane_id = p.id
		LEFT JOIN fleet_plane_runtime_config_states rcs
			ON rcs.plane_id = p.id
	` + suffix
}

func scanPlaneDetail(scanner interface{ Scan(dest ...any) error }) (plane.Detail, error) {
	var item plane.Detail
	var heartbeat sql.NullTime
	var syncAt sql.NullTime
	var registrationLastVerifiedAt sql.NullTime
	var registrationUpdatedAt sql.NullTime
	var operationUpdatedAt time.Time
	var snapshotID sql.NullString
	var snapshotPlaneID sql.NullString
	var snapshotNodesTotal sql.NullInt64
	var snapshotNodesReady sql.NullInt64
	var snapshotServicesTotal sql.NullInt64
	var snapshotRunsTotal sql.NullInt64
	var snapshotCPUMilliCapacity sql.NullInt64
	var snapshotCPUMilliAllocated sql.NullInt64
	var snapshotMemoryMiCapacity sql.NullInt64
	var snapshotMemoryMiAllocated sql.NullInt64
	var snapshotCapturedAt sql.NullTime
	var runtimePlaneID sql.NullString
	var runtimeSyncVersion sql.NullInt64
	var runtimeObservedAt sql.NullTime
	var runtimeNodesTotal sql.NullInt64
	var runtimeNodesReady sql.NullInt64
	var runtimeCPUMilliCapacity sql.NullInt64
	var runtimeCPUMilliAllocated sql.NullInt64
	var runtimeMemoryMiCapacity sql.NullInt64
	var runtimeMemoryMiAllocated sql.NullInt64
	var runtimeUpdatedAt sql.NullTime
	var runtimeConfigPlaneID sql.NullString
	var runtimeConfigObservedAt sql.NullTime
	var runtimeConfigFingerprint sql.NullString
	var runtimeConfigSummary []byte
	var runtimeConfigUpdatedAt sql.NullTime

	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.DisplayName,
		&item.Provider,
		&item.Region,
		&item.GRPCEndpoint,
		&item.CreatedAt,
		&item.Status.PlaneID,
		&item.Status.Status,
		&item.Status.Message,
		&heartbeat,
		&syncAt,
		&item.Status.UpdatedAt,
		&registrationLastVerifiedAt,
		&registrationUpdatedAt,
		&item.Operation.PlaneID,
		&item.Operation.State,
		&item.Operation.Reason,
		&operationUpdatedAt,
		&snapshotID,
		&snapshotPlaneID,
		&snapshotNodesTotal,
		&snapshotNodesReady,
		&snapshotServicesTotal,
		&snapshotRunsTotal,
		&snapshotCPUMilliCapacity,
		&snapshotCPUMilliAllocated,
		&snapshotMemoryMiCapacity,
		&snapshotMemoryMiAllocated,
		&snapshotCapturedAt,
		&runtimePlaneID,
		&runtimeSyncVersion,
		&runtimeObservedAt,
		&runtimeNodesTotal,
		&runtimeNodesReady,
		&runtimeCPUMilliCapacity,
		&runtimeCPUMilliAllocated,
		&runtimeMemoryMiCapacity,
		&runtimeMemoryMiAllocated,
		&runtimeUpdatedAt,
		&runtimeConfigPlaneID,
		&runtimeConfigObservedAt,
		&runtimeConfigFingerprint,
		&runtimeConfigSummary,
		&runtimeConfigUpdatedAt,
	); err != nil {
		return plane.Detail{}, err
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		item.Status.LastHeartbeatAt = &value
	}
	if syncAt.Valid {
		value := syncAt.Time
		item.Status.LastSyncAt = &value
	}
	item.Registration.Registered = registrationUpdatedAt.Valid
	if registrationLastVerifiedAt.Valid {
		value := registrationLastVerifiedAt.Time
		item.Registration.LastVerifiedAt = &value
	}
	if registrationUpdatedAt.Valid {
		value := registrationUpdatedAt.Time
		item.Registration.TokenUpdatedAt = &value
	}
	item.Operation.UpdatedAt = operationUpdatedAt
	if snapshotID.Valid {
		item.LatestCapacityRecord = &plane.CapacitySnapshot{
			ID:                snapshotID.String,
			PlaneID:           snapshotPlaneID.String,
			NodesTotal:        int(snapshotNodesTotal.Int64),
			NodesReady:        int(snapshotNodesReady.Int64),
			ServicesTotal:     int(snapshotServicesTotal.Int64),
			RunsTotal:         int(snapshotRunsTotal.Int64),
			CPUMilliCapacity:  int(snapshotCPUMilliCapacity.Int64),
			CPUMilliAllocated: int(snapshotCPUMilliAllocated.Int64),
			MemoryMiCapacity:  int(snapshotMemoryMiCapacity.Int64),
			MemoryMiAllocated: int(snapshotMemoryMiAllocated.Int64),
			CapturedAt:        snapshotCapturedAt.Time,
		}
	}
	if runtimePlaneID.Valid {
		item.LatestRuntimeInventory = &plane.RuntimeInventorySnapshot{
			PlaneID:           runtimePlaneID.String,
			SyncVersion:       runtimeSyncVersion.Int64,
			ObservedAt:        runtimeObservedAt.Time,
			NodesTotal:        int(runtimeNodesTotal.Int64),
			NodesReady:        int(runtimeNodesReady.Int64),
			CPUMilliCapacity:  int(runtimeCPUMilliCapacity.Int64),
			CPUMilliAllocated: int(runtimeCPUMilliAllocated.Int64),
			MemoryMiCapacity:  int(runtimeMemoryMiCapacity.Int64),
			MemoryMiAllocated: int(runtimeMemoryMiAllocated.Int64),
			UpdatedAt:         runtimeUpdatedAt.Time,
		}
	}
	if runtimeConfigPlaneID.Valid {
		item.LatestRuntimeConfig = &plane.RuntimeConfigSnapshot{
			PlaneID:     runtimeConfigPlaneID.String,
			ObservedAt:  runtimeConfigObservedAt.Time,
			Fingerprint: runtimeConfigFingerprint.String,
			UpdatedAt:   runtimeConfigUpdatedAt.Time,
		}
		if err := unmarshalJSON(runtimeConfigSummary, &item.LatestRuntimeConfig.Summary, map[string]any{}); err != nil {
			return plane.Detail{}, fmt.Errorf("unmarshal plane runtime config summary: %w", err)
		}
	}
	return item, nil
}

func scanPlaneOperation(scanner interface{ Scan(dest ...any) error }) (plane.Operation, error) {
	var item plane.Operation
	if err := scanner.Scan(
		&item.PlaneID,
		&item.State,
		&item.Reason,
		&item.UpdatedAt,
	); err != nil {
		return plane.Operation{}, err
	}
	return item, nil
}

func scanPlaneRegistration(scanner interface{ Scan(dest ...any) error }) (plane.Registration, error) {
	var item plane.Registration
	var lastVerifiedAt sql.NullTime
	var updatedAt sql.NullTime
	if err := scanner.Scan(&lastVerifiedAt, &updatedAt); err != nil {
		return plane.Registration{}, err
	}
	item.Registered = true
	if lastVerifiedAt.Valid {
		value := lastVerifiedAt.Time
		item.LastVerifiedAt = &value
	}
	if updatedAt.Valid {
		value := updatedAt.Time
		item.TokenUpdatedAt = &value
	}
	return item, nil
}

func scanPlaneStatus(scanner interface{ Scan(dest ...any) error }) (plane.PlaneStatus, error) {
	var item plane.PlaneStatus
	var heartbeat sql.NullTime
	var syncAt sql.NullTime
	if err := scanner.Scan(
		&item.PlaneID,
		&item.Status,
		&item.Message,
		&heartbeat,
		&syncAt,
		&item.UpdatedAt,
	); err != nil {
		return plane.PlaneStatus{}, err
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		item.LastHeartbeatAt = &value
	}
	if syncAt.Valid {
		value := syncAt.Time
		item.LastSyncAt = &value
	}
	return item, nil
}

func scanPlaneCapacitySnapshot(scanner interface{ Scan(dest ...any) error }) (plane.CapacitySnapshot, error) {
	var item plane.CapacitySnapshot
	if err := scanner.Scan(
		&item.ID,
		&item.PlaneID,
		&item.NodesTotal,
		&item.NodesReady,
		&item.ServicesTotal,
		&item.RunsTotal,
		&item.CPUMilliCapacity,
		&item.CPUMilliAllocated,
		&item.MemoryMiCapacity,
		&item.MemoryMiAllocated,
		&item.CapturedAt,
	); err != nil {
		return plane.CapacitySnapshot{}, fmt.Errorf("scan plane capacity snapshot: %w", err)
	}
	return item, nil
}
