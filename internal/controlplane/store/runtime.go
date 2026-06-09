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
	Nodes             []model.PlaneNode
}

func (in RecordRuntimeInventoryInput) resolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func (s *Store) ReplacePlaneRuntimeInventory(ctx context.Context, planeID string, input RecordRuntimeInventoryInput) error {
	observedAt := input.resolvedObservedAt(time.Now().UTC())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace plane runtime inventory tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
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
	); err != nil {
		return fmt.Errorf("upsert plane runtime inventory state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fleet_plane_statuses
		SET
			last_inventory_version = $2,
			updated_at = now()
		WHERE plane_id = $1
	`, planeID, input.SyncVersion); err != nil {
		return fmt.Errorf("update plane last inventory version: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM fleet_plane_nodes WHERE plane_id = $1`, planeID); err != nil {
		return fmt.Errorf("delete previous plane nodes: %w", err)
	}

	for _, item := range input.Nodes {
		lastHeartbeatAt := nullTime(item.LastHeartbeatAt)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fleet_plane_nodes (
				plane_id,
				node_id,
				name,
				provider,
				region,
				instance_id,
				instance_type,
				status,
				schedulable,
				elastic,
				cpu_milli_capacity,
				cpu_milli_allocated,
				memory_mi_capacity,
				memory_mi_allocated,
				last_heartbeat_at,
				observed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		`,
			planeID,
			item.NodeID,
			item.Name,
			item.Provider,
			item.Region,
			item.InstanceID,
			item.InstanceType,
			item.Status,
			item.Schedulable,
			item.Elastic,
			item.CPUMilliCapacity,
			item.CPUMilliAllocated,
			item.MemoryMiCapacity,
			item.MemoryMiAllocated,
			lastHeartbeatAt,
			observedAt,
		); err != nil {
			return fmt.Errorf("insert plane node: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace plane runtime inventory tx: %w", err)
	}
	return nil
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}
