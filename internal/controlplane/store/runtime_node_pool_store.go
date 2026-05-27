package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mini-cloud/internal/controlplane/runtimepool"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrRuntimeNodePoolNotFound = errors.New("runtime node pool not found")

func (s *Store) UpsertRuntimeNodePool(ctx context.Context, planeID string, input runtimepool.UpsertInput) (runtimepool.Pool, error) {
	item := runtimepool.Pool{
		PlaneID:          planeID,
		MinReady:         input.MinReady,
		MaxReady:         input.MaxReady,
		HeadroomCPUMilli: input.HeadroomCPUMilli,
		HeadroomMemoryMi: input.HeadroomMemoryMi,
	}
	if err := item.Validate(); err != nil {
		return runtimepool.Pool{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO fleet_runtime_node_pools (
			plane_id,
			min_ready,
			max_ready,
			headroom_cpu_milli,
			headroom_memory_mi
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (plane_id)
		DO UPDATE SET
			min_ready = EXCLUDED.min_ready,
			max_ready = EXCLUDED.max_ready,
			headroom_cpu_milli = EXCLUDED.headroom_cpu_milli,
			headroom_memory_mi = EXCLUDED.headroom_memory_mi,
			updated_at = now()
		RETURNING
			plane_id,
			min_ready,
			max_ready,
			headroom_cpu_milli,
			headroom_memory_mi,
			created_at,
			updated_at
	`, item.PlaneID, item.MinReady, item.MaxReady, item.HeadroomCPUMilli, item.HeadroomMemoryMi)

	pool, err := scanRuntimeNodePool(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return runtimepool.Pool{}, ErrPlaneNotFound
		}
		return runtimepool.Pool{}, fmt.Errorf("upsert runtime node pool: %w", err)
	}
	return pool, nil
}

func (s *Store) GetRuntimeNodePoolByPlane(ctx context.Context, planeID string) (runtimepool.Pool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			plane_id,
			min_ready,
			max_ready,
			headroom_cpu_milli,
			headroom_memory_mi,
			created_at,
			updated_at
		FROM fleet_runtime_node_pools
		WHERE plane_id = $1
	`, planeID)

	item, err := scanRuntimeNodePool(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimepool.Pool{}, ErrRuntimeNodePoolNotFound
		}
		return runtimepool.Pool{}, fmt.Errorf("query runtime node pool: %w", err)
	}
	return item, nil
}

func (s *Store) ListRuntimeNodePools(ctx context.Context) ([]runtimepool.Pool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			plane_id,
			min_ready,
			max_ready,
			headroom_cpu_milli,
			headroom_memory_mi,
			created_at,
			updated_at
		FROM fleet_runtime_node_pools
		ORDER BY created_at ASC, plane_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query control runtime node pools: %w", err)
	}
	defer closeRows(rows)

	items := make([]runtimepool.Pool, 0)
	for rows.Next() {
		item, err := scanRuntimeNodePool(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control runtime node pools: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteRuntimeNodePoolByPlane(ctx context.Context, planeID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM fleet_runtime_node_pools
		WHERE plane_id = $1
	`, planeID)
	if err != nil {
		return fmt.Errorf("delete runtime node pool: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrRuntimeNodePoolNotFound
	}
	return nil
}

func scanRuntimeNodePool(scanner interface{ Scan(dest ...any) error }) (runtimepool.Pool, error) {
	var item runtimepool.Pool
	if err := scanner.Scan(
		&item.PlaneID,
		&item.MinReady,
		&item.MaxReady,
		&item.HeadroomCPUMilli,
		&item.HeadroomMemoryMi,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return runtimepool.Pool{}, err
	}
	return item, nil
}
