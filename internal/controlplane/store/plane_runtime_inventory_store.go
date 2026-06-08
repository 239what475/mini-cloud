package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mini-cloud/internal/controlplane/model"
)

type RecordRuntimeInventoryInput struct {
	SyncVersion       int64
	ObservedAt        time.Time
	NodesTotal        int
	NodesReady        int
	CPUMilliCapacity  int
	CPUMilliAllocated int
	MemoryMiCapacity  int
	MemoryMiAllocated int
	Nodes             []model.RuntimeNode
}

func (in RecordRuntimeInventoryInput) resolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func (s *Store) ReplacePlaneRuntimeInventory(ctx context.Context, planeID string, input RecordRuntimeInventoryInput) (model.RuntimeInventorySnapshot, []model.RuntimeNode, error) {
	observedAt := input.resolvedObservedAt(time.Now().UTC())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("begin replace plane runtime inventory tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var state model.RuntimeInventorySnapshot
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
		return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("upsert plane runtime inventory state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fleet_plane_statuses
		SET
			last_inventory_version = $2,
			updated_at = now()
		WHERE plane_id = $1
	`, planeID, input.SyncVersion); err != nil {
		return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("update plane last inventory version: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM fleet_plane_runtime_nodes WHERE plane_id = $1`, planeID); err != nil {
		return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("delete previous plane runtime nodes: %w", err)
	}

	nodes := make([]model.RuntimeNode, 0, len(input.Nodes))
	for _, item := range input.Nodes {
		lastHeartbeatAt := nullTime(item.LastHeartbeatAt)
		var stored model.RuntimeNode
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
			return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("insert plane runtime node: %w", err)
		}
		if lastHeartbeatAt.Valid {
			value := lastHeartbeatAt.Time
			stored.LastHeartbeatAt = &value
		}
		nodes = append(nodes, stored)
	}

	if err := tx.Commit(); err != nil {
		return model.RuntimeInventorySnapshot{}, nil, fmt.Errorf("commit replace plane runtime inventory tx: %w", err)
	}

	return state, nodes, nil
}

func (s *Store) GetPlaneRuntimeInventorySnapshot(ctx context.Context, planeID string) (*model.RuntimeInventorySnapshot, error) {
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

func (s *Store) ListPlaneRuntimeNodes(ctx context.Context) ([]model.RuntimeNode, error) {
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

	items := make([]model.RuntimeNode, 0)
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

func (s *Store) ListPlaneRuntimeNodesByPlane(ctx context.Context, planeID string) ([]model.RuntimeNode, error) {
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

	items := make([]model.RuntimeNode, 0)
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

func scanPlaneRuntimeInventorySnapshot(scanner interface{ Scan(dest ...any) error }) (model.RuntimeInventorySnapshot, error) {
	var item model.RuntimeInventorySnapshot
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
		return model.RuntimeInventorySnapshot{}, err
	}
	return item, nil
}

func scanPlaneRuntimeNode(scanner interface{ Scan(dest ...any) error }) (model.RuntimeNode, error) {
	var item model.RuntimeNode
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
		return model.RuntimeNode{}, err
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	return item, nil
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}
