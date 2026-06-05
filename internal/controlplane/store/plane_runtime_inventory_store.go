package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
	controlservice "mini-cloud/internal/controlplane/service"
)

func (s *Store) ReplacePlaneRuntimeInventory(ctx context.Context, planeID string, input plane.RecordRuntimeInventoryInput) (plane.RuntimeInventorySnapshot, []plane.RuntimeNode, error) {
	observedAt := input.ResolvedObservedAt(time.Now().UTC())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("begin replace plane runtime inventory tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var state plane.RuntimeInventorySnapshot
	err = tx.QueryRowContext(ctx, `
		INSERT INTO fleet_plane_runtime_inventory_states (
			plane_id,
			sync_version,
			observed_at,
			nodes_total,
			nodes_ready,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (plane_id) DO UPDATE
		SET
			sync_version = EXCLUDED.sync_version,
			observed_at = EXCLUDED.observed_at,
			nodes_total = EXCLUDED.nodes_total,
			nodes_ready = EXCLUDED.nodes_ready,
			cpu_milli_capacity = EXCLUDED.cpu_milli_capacity,
			cpu_milli_allocated = EXCLUDED.cpu_milli_allocated,
			memory_mi_capacity = EXCLUDED.memory_mi_capacity,
			memory_mi_allocated = EXCLUDED.memory_mi_allocated,
			updated_at = now()
		RETURNING
			plane_id,
			sync_version,
			observed_at,
			nodes_total,
			nodes_ready,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			updated_at
	`,
		planeID,
		input.SyncVersion,
		observedAt,
		input.NodesTotal,
		input.NodesReady,
		input.CPUMilliCapacity,
		input.CPUMilliAllocated,
		input.MemoryMiCapacity,
		input.MemoryMiAllocated,
	).Scan(
		&state.PlaneID,
		&state.SyncVersion,
		&state.ObservedAt,
		&state.NodesTotal,
		&state.NodesReady,
		&state.CPUMilliCapacity,
		&state.CPUMilliAllocated,
		&state.MemoryMiCapacity,
		&state.MemoryMiAllocated,
		&state.UpdatedAt,
	)
	if err != nil {
		return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("upsert plane runtime inventory state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fleet_plane_statuses
		SET
			last_inventory_version = $2,
			updated_at = now()
		WHERE plane_id = $1
	`, planeID, input.SyncVersion); err != nil {
		return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("update plane last inventory version: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM fleet_plane_runtime_nodes WHERE plane_id = $1`, planeID); err != nil {
		return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("delete previous plane runtime nodes: %w", err)
	}

	nodes := make([]plane.RuntimeNode, 0, len(input.Nodes))
	for _, item := range input.Nodes {
		lastHeartbeatAt := nullTime(item.LastHeartbeatAt)
		var stored plane.RuntimeNode
		err := tx.QueryRowContext(ctx, `
			INSERT INTO fleet_plane_runtime_nodes (
				plane_id,
				node_id,
				node_epoch,
				name,
				provider,
				region,
				instance_id,
				instance_type,
				status,
				schedulable,
				cpu_milli_capacity,
				cpu_milli_allocated,
				memory_mi_capacity,
				memory_mi_allocated,
				last_heartbeat_at,
				observed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING
				plane_id,
				node_id,
				node_epoch,
				name,
				provider,
				region,
				instance_id,
				instance_type,
				status,
				schedulable,
				cpu_milli_capacity,
				cpu_milli_allocated,
				memory_mi_capacity,
				memory_mi_allocated,
				last_heartbeat_at,
				observed_at,
				updated_at
		`,
			planeID,
			item.NodeID,
			item.NodeEpoch,
			item.Name,
			item.Provider,
			item.Region,
			item.InstanceID,
			item.InstanceType,
			item.Status,
			item.Schedulable,
			item.CPUMilliCapacity,
			item.CPUMilliAllocated,
			item.MemoryMiCapacity,
			item.MemoryMiAllocated,
			lastHeartbeatAt,
			observedAt,
		).Scan(
			&stored.PlaneID,
			&stored.NodeID,
			&stored.NodeEpoch,
			&stored.Name,
			&stored.Provider,
			&stored.Region,
			&stored.InstanceID,
			&stored.InstanceType,
			&stored.Status,
			&stored.Schedulable,
			&stored.CPUMilliCapacity,
			&stored.CPUMilliAllocated,
			&stored.MemoryMiCapacity,
			&stored.MemoryMiAllocated,
			&lastHeartbeatAt,
			&stored.ObservedAt,
			&stored.UpdatedAt,
		)
		if err != nil {
			return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("insert plane runtime node: %w", err)
		}
		if lastHeartbeatAt.Valid {
			value := lastHeartbeatAt.Time
			stored.LastHeartbeatAt = &value
		}
		nodes = append(nodes, stored)
	}

	if err := tx.Commit(); err != nil {
		return plane.RuntimeInventorySnapshot{}, nil, fmt.Errorf("commit replace plane runtime inventory tx: %w", err)
	}

	return state, nodes, nil
}

func (s *Store) GetPlaneRuntimeInventorySnapshot(ctx context.Context, planeID string) (*plane.RuntimeInventorySnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			plane_id,
			sync_version,
			observed_at,
			nodes_total,
			nodes_ready,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			updated_at
		FROM fleet_plane_runtime_inventory_states
		WHERE plane_id = $1
	`, planeID)
	item, err := scanPlaneRuntimeInventorySnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query plane runtime inventory state: %w", err)
	}
	return &item, nil
}

func (s *Store) ListPlaneRuntimeNodes(ctx context.Context) ([]plane.RuntimeNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			plane_id,
			node_id,
			node_epoch,
			name,
			provider,
			region,
			instance_id,
			instance_type,
			status,
			schedulable,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			last_heartbeat_at,
			observed_at,
			updated_at
		FROM fleet_plane_runtime_nodes
		ORDER BY plane_id ASC, node_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query plane runtime nodes: %w", err)
	}
	defer closeRows(rows)

	items := make([]plane.RuntimeNode, 0)
	for rows.Next() {
		item, err := scanPlaneRuntimeNode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plane runtime nodes: %w", err)
	}
	return items, nil
}

func (s *Store) ListPlaneRuntimeNodesByPlane(ctx context.Context, planeID string) ([]plane.RuntimeNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			plane_id,
			node_id,
			node_epoch,
			name,
			provider,
			region,
			instance_id,
			instance_type,
			status,
			schedulable,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated,
			last_heartbeat_at,
			observed_at,
			updated_at
		FROM fleet_plane_runtime_nodes
		WHERE plane_id = $1
		ORDER BY node_id ASC
	`, planeID)
	if err != nil {
		return nil, fmt.Errorf("query plane runtime nodes by plane: %w", err)
	}
	defer closeRows(rows)

	items := make([]plane.RuntimeNode, 0)
	for rows.Next() {
		item, err := scanPlaneRuntimeNode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plane runtime nodes by plane: %w", err)
	}
	return items, nil
}

func (s *Store) UpsertServiceCellAssignment(ctx context.Context, input controlservice.CellAssignment) (controlservice.CellAssignment, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO fleet_service_cell_assignments (
			service_id,
			cell_key,
			plane_id,
			target_node_id,
			target_node_epoch,
			inventory_version,
			cpu_milli_reserved,
			memory_mi_reserved,
			state,
			last_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (service_id, cell_key) DO UPDATE
		SET
			plane_id = EXCLUDED.plane_id,
			target_node_id = EXCLUDED.target_node_id,
			target_node_epoch = EXCLUDED.target_node_epoch,
			inventory_version = EXCLUDED.inventory_version,
			cpu_milli_reserved = EXCLUDED.cpu_milli_reserved,
			memory_mi_reserved = EXCLUDED.memory_mi_reserved,
			state = EXCLUDED.state,
			last_error = EXCLUDED.last_error,
			updated_at = now()
		RETURNING
			service_id,
			cell_key,
			plane_id,
			target_node_id,
			target_node_epoch,
			inventory_version,
			cpu_milli_reserved,
			memory_mi_reserved,
			state,
			last_error,
			created_at,
			updated_at
	`,
		input.ServiceID,
		input.CellKey,
		input.PlaneID,
		input.TargetNodeID,
		input.TargetNodeEpoch,
		input.InventoryVersion,
		input.CPUMilliReserved,
		input.MemoryMiReserved,
		input.State,
		input.LastError,
	)
	item, err := scanServiceCellAssignment(row)
	if err != nil {
		return controlservice.CellAssignment{}, fmt.Errorf("upsert service cell assignment: %w", err)
	}
	return item, nil
}

func (s *Store) GetServiceCellAssignment(ctx context.Context, serviceID string, cellKey string) (controlservice.CellAssignment, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			service_id,
			cell_key,
			plane_id,
			target_node_id,
			target_node_epoch,
			inventory_version,
			cpu_milli_reserved,
			memory_mi_reserved,
			state,
			last_error,
			created_at,
			updated_at
		FROM fleet_service_cell_assignments
		WHERE service_id = $1 AND cell_key = $2
		LIMIT 1
	`, serviceID, cellKey)
	item, err := scanServiceCellAssignment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return controlservice.CellAssignment{}, ErrServiceCellPlacementNotFound
		}
		return controlservice.CellAssignment{}, fmt.Errorf("query service cell assignment: %w", err)
	}
	return item, nil
}

func (s *Store) ListServiceCellAssignmentsByCell(ctx context.Context, serviceID string, cellKey string) ([]controlservice.CellAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			service_id,
			cell_key,
			plane_id,
			target_node_id,
			target_node_epoch,
			inventory_version,
			cpu_milli_reserved,
			memory_mi_reserved,
			state,
			last_error,
			created_at,
			updated_at
		FROM fleet_service_cell_assignments
		WHERE service_id = $1 AND cell_key = $2
		ORDER BY created_at ASC
	`, serviceID, cellKey)
	if err != nil {
		return nil, fmt.Errorf("query service cell assignments by cell: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.CellAssignment, 0)
	for rows.Next() {
		item, err := scanServiceCellAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service cell assignments by cell: %w", err)
	}
	return items, nil
}

func (s *Store) ListServiceCellAssignments(ctx context.Context) ([]controlservice.CellAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			service_id,
			cell_key,
			plane_id,
			target_node_id,
			target_node_epoch,
			inventory_version,
			cpu_milli_reserved,
			memory_mi_reserved,
			state,
			last_error,
			created_at,
			updated_at
		FROM fleet_service_cell_assignments
		ORDER BY service_id ASC, cell_key ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query service cell assignments: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.CellAssignment, 0)
	for rows.Next() {
		item, err := scanServiceCellAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service cell assignments: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteServiceCellAssignment(ctx context.Context, serviceID string, cellKey string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM fleet_service_cell_assignments
		WHERE service_id = $1 AND cell_key = $2
	`, serviceID, cellKey)
	if err != nil {
		return fmt.Errorf("delete service cell assignment: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServiceCellPlacementNotFound
	}
	return nil
}

func scanPlaneRuntimeInventorySnapshot(scanner interface{ Scan(dest ...any) error }) (plane.RuntimeInventorySnapshot, error) {
	var item plane.RuntimeInventorySnapshot
	if err := scanner.Scan(
		&item.PlaneID,
		&item.SyncVersion,
		&item.ObservedAt,
		&item.NodesTotal,
		&item.NodesReady,
		&item.CPUMilliCapacity,
		&item.CPUMilliAllocated,
		&item.MemoryMiCapacity,
		&item.MemoryMiAllocated,
		&item.UpdatedAt,
	); err != nil {
		return plane.RuntimeInventorySnapshot{}, err
	}
	return item, nil
}

func scanPlaneRuntimeNode(scanner interface{ Scan(dest ...any) error }) (plane.RuntimeNode, error) {
	var item plane.RuntimeNode
	var lastHeartbeatAt sql.NullTime
	if err := scanner.Scan(
		&item.PlaneID,
		&item.NodeID,
		&item.NodeEpoch,
		&item.Name,
		&item.Provider,
		&item.Region,
		&item.InstanceID,
		&item.InstanceType,
		&item.Status,
		&item.Schedulable,
		&item.CPUMilliCapacity,
		&item.CPUMilliAllocated,
		&item.MemoryMiCapacity,
		&item.MemoryMiAllocated,
		&lastHeartbeatAt,
		&item.ObservedAt,
		&item.UpdatedAt,
	); err != nil {
		return plane.RuntimeNode{}, err
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	return item, nil
}

func scanServiceCellAssignment(scanner interface{ Scan(dest ...any) error }) (controlservice.CellAssignment, error) {
	var item controlservice.CellAssignment
	if err := scanner.Scan(
		&item.ServiceID,
		&item.CellKey,
		&item.PlaneID,
		&item.TargetNodeID,
		&item.TargetNodeEpoch,
		&item.InventoryVersion,
		&item.CPUMilliReserved,
		&item.MemoryMiReserved,
		&item.State,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.CellAssignment{}, err
	}
	return item, nil
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}
