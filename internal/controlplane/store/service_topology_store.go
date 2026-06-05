package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	controlservice "mini-cloud/internal/controlplane/service"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrServicePlacementNotFound = errors.New("service selection not found")

func (s *Store) UpsertServicePlacement(ctx context.Context, input controlservice.ServicePlacement) (controlservice.ServicePlacement, error) {
	return s.upsertServicePlacement(ctx, input, nil)
}

func (s *Store) UpsertServicePlacementForGeneration(ctx context.Context, input controlservice.ServicePlacement, expectedGeneration int64) (controlservice.ServicePlacement, error) {
	return s.upsertServicePlacement(ctx, input, &expectedGeneration)
}

func (s *Store) upsertServicePlacement(ctx context.Context, input controlservice.ServicePlacement, expectedGeneration *int64) (controlservice.ServicePlacement, error) {
	return upsertServicePlacementTx(ctx, s, s.db, input, expectedGeneration)
}

func upsertServicePlacementTx(ctx context.Context, stores *Store, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, input controlservice.ServicePlacement, expectedGeneration *int64) (controlservice.ServicePlacement, error) {
	query := `
		INSERT INTO fleet_service_placements (
			service_id,
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
			$5
		WHERE EXISTS (
			SELECT 1
			FROM fleet_services s
			WHERE s.id = $1
	`
	args := []any{
		input.ServiceID,
		input.PlaneID,
		input.RemoteStatus,
		input.RemoteHealthy,
		input.RemoteMessage,
	}
	if expectedGeneration != nil {
		query += ` AND s.generation = $6`
		args = append(args, *expectedGeneration)
	}
	query += `
		)
		ON CONFLICT (service_id)
		DO UPDATE SET
			plane_id = EXCLUDED.plane_id,
			remote_status = EXCLUDED.remote_status,
			remote_healthy = EXCLUDED.remote_healthy,
			remote_message = EXCLUDED.remote_message,
			updated_at = now()
		RETURNING
			service_id,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message,
			created_at,
			updated_at
	`
	item, err := scanServicePlacement(queryer.QueryRowContext(ctx, query, args...))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "fleet_service_placements_service_id_fkey":
				return controlservice.ServicePlacement{}, ErrServiceNotFound
			case "fleet_service_placements_plane_id_fkey":
				return controlservice.ServicePlacement{}, ErrPlaneNotFound
			}
		}
		if errors.Is(err, sql.ErrNoRows) && expectedGeneration != nil {
			return controlservice.ServicePlacement{}, classifyServiceGenerationConflict(ctx, stores, input.ServiceID, *expectedGeneration)
		}
		return controlservice.ServicePlacement{}, fmt.Errorf("upsert service placement: %w", err)
	}
	return item, nil
}

func (s *Store) GetServicePlacement(ctx context.Context, serviceID string) (controlservice.ServicePlacement, error) {
	item, err := scanServicePlacement(s.db.QueryRowContext(ctx, `
		SELECT
			service_id,
			plane_id,
			remote_status,
			remote_healthy,
			remote_message,
			created_at,
			updated_at
		FROM fleet_service_placements
		WHERE service_id = $1
	`, serviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.ServicePlacement{}, ErrServicePlacementNotFound
		}
		return controlservice.ServicePlacement{}, fmt.Errorf("query service placement: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteServicePlacement(ctx context.Context, serviceID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM fleet_service_placements
		WHERE service_id = $1
	`, serviceID)
	if err != nil {
		return fmt.Errorf("delete service placement: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServicePlacementNotFound
	}
	return nil
}

func (s *Store) DeleteServicePlacementForGeneration(ctx context.Context, serviceID string, expectedGeneration int64) error {
	row := s.db.QueryRowContext(ctx, `
		DELETE FROM fleet_service_placements AS p
		USING fleet_services AS s
		WHERE p.service_id = $1
			AND s.id = p.service_id
			AND s.generation = $2
		RETURNING p.service_id
	`, serviceID, expectedGeneration)
	var deletedServiceID string
	if err := row.Scan(&deletedServiceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			current, currentErr := s.GetService(ctx, serviceID)
			if currentErr != nil {
				return currentErr
			}
			if current.Metadata.Generation != expectedGeneration {
				return ErrServiceGenerationConflict
			}
			if _, getErr := s.GetServicePlacement(ctx, serviceID); getErr != nil {
				return getErr
			}
			return ErrServiceGenerationConflict
		}
		return fmt.Errorf("delete service selection for generation: %w", err)
	}
	return nil
}

func scanServicePlacement(scanner interface{ Scan(dest ...any) error }) (controlservice.ServicePlacement, error) {
	var item controlservice.ServicePlacement
	if err := scanner.Scan(
		&item.ServiceID,
		&item.PlaneID,
		&item.RemoteStatus,
		&item.RemoteHealthy,
		&item.RemoteMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.ServicePlacement{}, err
	}
	return item, nil
}
