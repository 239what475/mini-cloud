package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	controlservice "mini-cloud/internal/controlplane/service"
)

var ErrServiceCellNotFound = errors.New("service cell not found")

const serviceCellSelectColumns = `
	service_id,
	cell_key,
	role,
	spec_provider,
	spec_region,
	spec_pinned_plane_id,
	spec_instance_class,
	status_desired_state,
	status_observed_generation,
	status_phase,
	status_healthy,
	status_message,
	status_conditions_json,
	status_last_reconciled_at,
	created_at,
	updated_at
`

func (s *Store) ListServiceCellsByService(ctx context.Context, serviceID string) ([]controlservice.Cell, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceCellSelectColumns+`
		FROM fleet_service_cells
		WHERE service_id = $1
		ORDER BY CASE role WHEN 'primary' THEN 0 ELSE 1 END ASC, cell_key ASC
	`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query service cells: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.Cell, 0)
	for rows.Next() {
		item, err := scanServiceCell(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service cells: %w", err)
	}
	return items, nil
}

func (s *Store) GetServiceCell(ctx context.Context, serviceID string, cellKey string) (controlservice.Cell, error) {
	item, err := scanServiceCell(s.db.QueryRowContext(ctx, `
		SELECT `+serviceCellSelectColumns+`
		FROM fleet_service_cells
		WHERE service_id = $1 AND cell_key = $2
	`, serviceID, cellKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Cell{}, ErrServiceCellNotFound
		}
		return controlservice.Cell{}, fmt.Errorf("query service cell: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateServiceCellStatus(ctx context.Context, serviceID string, cellKey string, input controlservice.UpdateStatusInput) (controlservice.Cell, error) {
	return s.updateServiceCellStatus(ctx, serviceID, cellKey, nil, input)
}

func (s *Store) UpdateServiceCellStatusForGeneration(ctx context.Context, serviceID string, cellKey string, expectedGeneration int64, input controlservice.UpdateStatusInput) (controlservice.Cell, error) {
	return s.updateServiceCellStatus(ctx, serviceID, cellKey, &expectedGeneration, input)
}

func (s *Store) updateServiceCellStatus(ctx context.Context, serviceID string, cellKey string, expectedGeneration *int64, input controlservice.UpdateStatusInput) (controlservice.Cell, error) {
	current, err := s.GetServiceCell(ctx, serviceID, cellKey)
	if err != nil {
		return controlservice.Cell{}, err
	}

	mergedConditions := mergeServiceConditions(current.Status.Conditions, input.Conditions)
	statusConditionsJSON, err := marshalJSON(mergedConditions, []controlservice.Condition{})
	if err != nil {
		return controlservice.Cell{}, fmt.Errorf("marshal service cell status conditions: %w", err)
	}

	query := `
		UPDATE fleet_service_cells AS c
		SET
			status_observed_generation = $3,
			status_phase = $4,
			status_healthy = $5,
			status_message = $6,
			status_conditions_json = $7,
			status_last_reconciled_at = $8,
			updated_at = now()
		WHERE c.service_id = $1
			AND c.cell_key = $2
	`
	args := []any{
		serviceID,
		cellKey,
		input.ObservedGeneration,
		input.Phase,
		input.Healthy,
		input.Message,
		statusConditionsJSON,
		input.LastReconciledAt,
	}
	if expectedGeneration != nil {
		query += `
			AND EXISTS (
				SELECT 1
				FROM fleet_services s
				WHERE s.id = c.service_id
					AND s.generation = $9
			)
		`
		args = append(args, *expectedGeneration)
	}
	query += ` RETURNING ` + serviceCellSelectColumns

	item, err := scanServiceCell(s.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if expectedGeneration != nil {
				return controlservice.Cell{}, classifyServiceGenerationConflict(ctx, s, serviceID, *expectedGeneration)
			}
			return controlservice.Cell{}, ErrServiceCellNotFound
		}
		return controlservice.Cell{}, fmt.Errorf("update service cell status: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteServiceCell(ctx context.Context, serviceID string, cellKey string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM fleet_service_cells
		WHERE service_id = $1 AND cell_key = $2
	`, serviceID, cellKey)
	if err != nil {
		return fmt.Errorf("delete service cell: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServiceCellNotFound
	}
	return nil
}

func (s *Store) DeleteServiceCellForGeneration(ctx context.Context, serviceID string, cellKey string, expectedGeneration int64) error {
	row := s.db.QueryRowContext(ctx, `
		DELETE FROM fleet_service_cells AS c
		USING fleet_services AS s
		WHERE c.service_id = $1
			AND c.cell_key = $2
			AND s.id = c.service_id
			AND s.generation = $3
		RETURNING c.service_id
	`, serviceID, cellKey, expectedGeneration)
	var deletedServiceID string
	if err := row.Scan(&deletedServiceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return classifyServiceCellGenerationConflict(ctx, s, serviceID, cellKey, expectedGeneration)
		}
		return fmt.Errorf("delete service cell for generation: %w", err)
	}
	return nil
}

func scanServiceCell(scanner interface{ Scan(dest ...any) error }) (controlservice.Cell, error) {
	var item controlservice.Cell
	var pinnedPlaneID sql.NullString
	var conditionsJSON []byte
	var lastReconciledAt sql.NullTime
	if err := scanner.Scan(
		&item.ServiceID,
		&item.Key,
		&item.Role,
		&item.Provider,
		&item.Region,
		&pinnedPlaneID,
		&item.InstanceClass,
		&item.DesiredState,
		&item.Status.ObservedGeneration,
		&item.Status.Phase,
		&item.Status.Healthy,
		&item.Status.Message,
		&conditionsJSON,
		&lastReconciledAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.Cell{}, err
	}
	if pinnedPlaneID.Valid {
		item.PinnedPlaneID = pinnedPlaneID.String
	}
	if err := unmarshalJSON(conditionsJSON, &item.Status.Conditions, []controlservice.Condition{}); err != nil {
		return controlservice.Cell{}, fmt.Errorf("decode service cell status conditions: %w", err)
	}
	if lastReconciledAt.Valid {
		lastValue := lastReconciledAt.Time.UTC()
		item.Status.LastReconciledAt = &lastValue
	}
	return item, nil
}

func classifyServiceCellGenerationConflict(ctx context.Context, stores *Store, serviceID string, cellKey string, expectedGeneration int64) error {
	current, err := stores.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	if current.Metadata.Generation != expectedGeneration {
		return ErrServiceGenerationConflict
	}
	if _, err := stores.GetServiceCell(ctx, serviceID, cellKey); err != nil {
		return err
	}
	return ErrServiceGenerationConflict
}
