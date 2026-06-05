package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	controlservice "mini-cloud/internal/controlplane/service"
)

var ErrServiceRunNotFound = errors.New("service run not found")

const serviceRunSelectColumns = `
	id,
	service_id,
	generation,
	plan_id,
	spec_json,
	desired_replicas,
	status,
	message,
	observed_at,
	created_at,
	updated_at
`

func (s *Store) CreateServiceRun(ctx context.Context, input controlservice.CreateRunInput) (controlservice.ServiceRun, error) {
	id := input.ID
	if id == "" {
		generated, err := newID("run")
		if err != nil {
			return controlservice.ServiceRun{}, err
		}
		id = generated
	}
	specJSON, err := marshalJSON(controlservice.CloneSpec(input.Spec), controlservice.Spec{})
	if err != nil {
		return controlservice.ServiceRun{}, fmt.Errorf("marshal service run spec: %w", err)
	}
	status := controlservice.NormalizeRunPhase(input.Status)
	return scanServiceRun(s.db.QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO fleet_service_runs (
				id,
				service_id,
				generation,
				plan_id,
				spec_json,
				desired_replicas,
				status,
				message,
				observed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (service_id, generation) DO NOTHING
			RETURNING `+serviceRunSelectColumns+`
		)
		SELECT `+serviceRunSelectColumns+` FROM inserted
		UNION ALL
		SELECT `+serviceRunSelectColumns+`
		FROM fleet_service_runs
		WHERE service_id = $2
		  AND generation = $3
		  AND NOT EXISTS (SELECT 1 FROM inserted)
		LIMIT 1
	`, id, input.ServiceID, input.Generation, input.PlanID, specJSON, input.DesiredReplicas, status, input.Message, input.ObservedAt))
}

func (s *Store) GetServiceRunByGeneration(ctx context.Context, serviceID string, generation int64) (controlservice.ServiceRun, error) {
	item, err := scanServiceRun(s.db.QueryRowContext(ctx, `
		SELECT `+serviceRunSelectColumns+`
		FROM fleet_service_runs
		WHERE service_id = $1 AND generation = $2
	`, serviceID, generation))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.ServiceRun{}, ErrServiceRunNotFound
		}
		return controlservice.ServiceRun{}, fmt.Errorf("query service run by generation: %w", err)
	}
	return item, nil
}

func (s *Store) ListServiceRuns(ctx context.Context, serviceID string) ([]controlservice.ServiceRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceRunSelectColumns+`
		FROM fleet_service_runs
		WHERE service_id = $1
		ORDER BY generation ASC, created_at ASC
	`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query service runs: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.ServiceRun, 0)
	for rows.Next() {
		item, err := scanServiceRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service runs: %w", err)
	}
	return items, nil
}

func (s *Store) UpdateServiceRun(ctx context.Context, serviceID string, generation int64, input controlservice.UpdateRunInput) (controlservice.ServiceRun, error) {
	item, err := scanServiceRun(s.db.QueryRowContext(ctx, `
		UPDATE fleet_service_runs
		SET
			status = $3,
			message = $4,
			observed_at = $5,
			updated_at = now()
		WHERE service_id = $1 AND generation = $2
		RETURNING `+serviceRunSelectColumns+`
	`, serviceID, generation, controlservice.NormalizeRunPhase(input.Status), input.Message, input.ObservedAt))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.ServiceRun{}, ErrServiceRunNotFound
		}
		return controlservice.ServiceRun{}, fmt.Errorf("update service run: %w", err)
	}
	return item, nil
}

func (s *Store) SupersedeServiceRunsBeforeGeneration(ctx context.Context, serviceID string, generation int64, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE fleet_service_runs
		SET
			status = $3,
			message = $4,
			updated_at = now()
		WHERE service_id = $1
		  AND generation < $2
		  AND status <> $3
	`, serviceID, generation, controlservice.RunPhaseSuperseded, message)
	if err != nil {
		return fmt.Errorf("supersede old service runs: %w", err)
	}
	return nil
}

func scanServiceRun(scanner interface{ Scan(dest ...any) error }) (controlservice.ServiceRun, error) {
	var item controlservice.ServiceRun
	var specJSON []byte
	var observedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.ServiceID,
		&item.Generation,
		&item.PlanID,
		&specJSON,
		&item.DesiredReplicas,
		&item.Status,
		&item.Message,
		&observedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.ServiceRun{}, err
	}
	if err := unmarshalJSON(specJSON, &item.Spec, controlservice.Spec{}); err != nil {
		return controlservice.ServiceRun{}, fmt.Errorf("decode service run spec: %w", err)
	}
	item.Spec = controlservice.CloneSpec(item.Spec)
	item.Status = controlservice.NormalizeRunPhase(item.Status)
	if observedAt.Valid {
		value := observedAt.Time.UTC()
		item.ObservedAt = &value
	}
	return item, nil
}
