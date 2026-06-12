package store

import (
	"context"
	"fmt"
	"time"

	"mini-cloud/internal/controlplane/model"
)

type RecordNodeInventoryInput struct {
	ObservedAt        time.Time
	NodesTotal        int
	NodesReady        int
	CPUMilliCapacity  int
	CPUMilliAllocated int
	MemoryMiCapacity  int
	MemoryMiAllocated int
	Nodes             []model.PlaneNode
}

func (in RecordNodeInventoryInput) resolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func (s *Store) ReplacePlaneNodeInventory(ctx context.Context, planeID string, input RecordNodeInventoryInput) error {
	observedAt := input.resolvedObservedAt(time.Now().UTC())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace plane node inventory tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plane_node_inventory_states (
			plane_id,
			observed_at,
			nodes_total,
			nodes_ready,
			cpu_milli_capacity,
			cpu_milli_allocated,
			memory_mi_capacity,
			memory_mi_allocated
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (plane_id) DO UPDATE
		SET
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
		observedAt,
		input.NodesTotal,
		input.NodesReady,
		input.CPUMilliCapacity,
		input.CPUMilliAllocated,
		input.MemoryMiCapacity,
		input.MemoryMiAllocated,
	); err != nil {
		return fmt.Errorf("upsert plane node inventory state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM plane_nodes WHERE plane_id = $1`, planeID); err != nil {
		return fmt.Errorf("delete previous plane nodes: %w", err)
	}

	for _, item := range input.Nodes {
		var lastHeartbeatAt any
		if item.LastHeartbeatAt != nil && !item.LastHeartbeatAt.IsZero() {
			lastHeartbeatAt = item.LastHeartbeatAt.UTC()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO plane_nodes (
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
		return fmt.Errorf("commit replace plane node inventory tx: %w", err)
	}
	return nil
}
