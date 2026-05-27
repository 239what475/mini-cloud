package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	controlservice "mini-cloud/internal/controlplane/service"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrServiceCellPlacementNotFound = errors.New("service cell selection not found")

func (s *Store) UpsertServiceCellPlacement(ctx context.Context, input controlservice.CellPlacement) (controlservice.CellPlacement, error) {
	return s.upsertServiceCellPlacement(ctx, input, nil)
}

func (s *Store) UpsertServiceCellPlacementForGeneration(ctx context.Context, input controlservice.CellPlacement, expectedGeneration int64) (controlservice.CellPlacement, error) {
	return s.upsertServiceCellPlacement(ctx, input, &expectedGeneration)
}

func (s *Store) upsertServiceCellPlacement(ctx context.Context, input controlservice.CellPlacement, expectedGeneration *int64) (controlservice.CellPlacement, error) {
	query := `
		INSERT INTO fleet_service_cell_placements (
			service_id,
			cell_key,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message
		)
		SELECT
			$1,
			$2,
			$3,
			$4,
			$5,
			$6
		WHERE EXISTS (
			SELECT 1
			FROM fleet_service_cells c
			JOIN fleet_services s ON s.id = c.service_id
			WHERE c.service_id = $1
				AND c.cell_key = $2
	`
	args := []any{
		input.ServiceID,
		input.CellKey,
		input.PlaneID,
		input.RemoteStatus,
		input.RemoteHealthy,
		input.RemoteMessage,
	}
	if expectedGeneration != nil {
		query += ` AND s.generation = $7`
		args = append(args, *expectedGeneration)
	}
	query += `
		)
		ON CONFLICT (service_id, cell_key)
		DO UPDATE SET
			plane_id = EXCLUDED.plane_id,
			remote_status = EXCLUDED.remote_status,
			remote_healthy = EXCLUDED.remote_healthy,
			remote_message = EXCLUDED.remote_message,
			updated_at = now()
		RETURNING
			service_id,
			cell_key,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message,
			created_at,
			updated_at
	`

	item, err := scanServiceCellPlacement(s.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "fleet_service_cell_placements_cell_fk":
				return controlservice.CellPlacement{}, ErrServiceCellNotFound
			case "fleet_service_cell_placements_plane_id_fkey":
				return controlservice.CellPlacement{}, ErrPlaneNotFound
			}
		}
		if errors.Is(err, sql.ErrNoRows) && expectedGeneration != nil {
			return controlservice.CellPlacement{}, classifyServiceGenerationConflict(ctx, s, input.ServiceID, *expectedGeneration)
		}
		return controlservice.CellPlacement{}, fmt.Errorf("upsert service cell placement: %w", err)
	}
	return item, nil
}

func (s *Store) ListServiceCellPlacementsByService(ctx context.Context, serviceID string) ([]controlservice.CellPlacement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			service_id,
			cell_key,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message,
			created_at,
			updated_at
		FROM fleet_service_cell_placements
		WHERE service_id = $1
		ORDER BY cell_key ASC
	`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query service cell placements: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.CellPlacement, 0)
	for rows.Next() {
		item, err := scanServiceCellPlacement(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service cell placements: %w", err)
	}
	return items, nil
}

func (s *Store) GetServiceCellPlacement(ctx context.Context, serviceID string, cellKey string) (controlservice.CellPlacement, error) {
	item, err := scanServiceCellPlacement(s.db.QueryRowContext(ctx, `
		SELECT
			service_id,
			cell_key,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message,
			created_at,
			updated_at
		FROM fleet_service_cell_placements
		WHERE service_id = $1 AND cell_key = $2
	`, serviceID, cellKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.CellPlacement{}, ErrServiceCellPlacementNotFound
		}
		return controlservice.CellPlacement{}, fmt.Errorf("query service cell placement: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteServiceCellPlacement(ctx context.Context, serviceID string, cellKey string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM fleet_service_cell_placements
		WHERE service_id = $1 AND cell_key = $2
	`, serviceID, cellKey)
	if err != nil {
		return fmt.Errorf("delete service cell placement: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServiceCellPlacementNotFound
	}
	return nil
}

func (s *Store) DeleteServiceCellPlacementForGeneration(ctx context.Context, serviceID string, cellKey string, expectedGeneration int64) error {
	row := s.db.QueryRowContext(ctx, `
		DELETE FROM fleet_service_cell_placements AS p
		USING fleet_services AS s
		WHERE p.service_id = $1
			AND p.cell_key = $2
			AND s.id = p.service_id
			AND s.generation = $3
		RETURNING p.service_id
	`, serviceID, cellKey, expectedGeneration)
	var deletedServiceID string
	if err := row.Scan(&deletedServiceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return classifyServiceCellPlacementGenerationConflict(ctx, s, serviceID, cellKey, expectedGeneration)
		}
		return fmt.Errorf("delete service cell selection for generation: %w", err)
	}
	return nil
}

func scanServiceCellPlacement(scanner interface{ Scan(dest ...any) error }) (controlservice.CellPlacement, error) {
	var item controlservice.CellPlacement
	if err := scanner.Scan(
		&item.ServiceID,
		&item.CellKey,
		&item.PlaneID,
		&item.RemoteStatus,
		&item.RemoteHealthy,
		&item.RemoteMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.CellPlacement{}, err
	}
	return item, nil
}

func classifyServiceCellPlacementGenerationConflict(ctx context.Context, stores *Store, serviceID string, cellKey string, expectedGeneration int64) error {
	current, err := stores.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	if current.Metadata.Generation != expectedGeneration {
		return ErrServiceGenerationConflict
	}
	if _, err := stores.GetServiceCellPlacement(ctx, serviceID, cellKey); err != nil {
		return err
	}
	return ErrServiceGenerationConflict
}
