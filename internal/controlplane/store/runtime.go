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

	if _, err := tx.ExecContext(ctx, `DELETE FROM fleet_plane_runtime_nodes WHERE plane_id = $1`, planeID); err != nil {
		return fmt.Errorf("delete previous plane runtime nodes: %w", err)
	}

	for _, item := range input.Nodes {
		lastHeartbeatAt := nullTime(item.LastHeartbeatAt)
		if _, err := tx.ExecContext(ctx, `
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
		); err != nil {
			return fmt.Errorf("insert plane runtime node: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace plane runtime inventory tx: %w", err)
	}
	return nil
}

type RecordRuntimeConfigInput struct {
	ObservedAt  time.Time
	Fingerprint string
	Summary     map[string]any
}

func (in RecordRuntimeConfigInput) resolvedObservedAt(now time.Time) time.Time {
	if in.ObservedAt.IsZero() {
		return now.UTC()
	}
	return in.ObservedAt.UTC()
}

func (s *Store) RecordPlaneRuntimeConfig(ctx context.Context, planeID string, input RecordRuntimeConfigInput) error {
	observedAt := input.resolvedObservedAt(time.Now().UTC())
	summaryJSON, err := marshalJSON(input.Summary, map[string]any{})
	if err != nil {
		return fmt.Errorf("marshal plane runtime config summary: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_plane_runtime_config_states (
			plane_id,
			observed_at,
			fingerprint,
			summary_json
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (plane_id) DO UPDATE
		SET
			observed_at = EXCLUDED.observed_at,
			fingerprint = EXCLUDED.fingerprint,
			summary_json = EXCLUDED.summary_json,
			updated_at = now()
		`, planeID, observedAt, input.Fingerprint, summaryJSON); err != nil {
		return fmt.Errorf("upsert plane runtime config state: %w", err)
	}
	return nil
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}
