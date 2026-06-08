package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mini-cloud/internal/controlplane/model"
)

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

func (s *Store) RecordPlaneRuntimeConfig(ctx context.Context, planeID string, input RecordRuntimeConfigInput) (model.RuntimeConfigSnapshot, error) {
	observedAt := input.resolvedObservedAt(time.Now().UTC())
	summaryJSON, err := marshalJSON(input.Summary, map[string]any{})
	if err != nil {
		return model.RuntimeConfigSnapshot{}, fmt.Errorf("marshal plane runtime config summary: %w", err)
	}

	row := s.db.QueryRowContext(ctx, `
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
		RETURNING
			plane_id,
			observed_at,
			fingerprint,
			summary_json,
			updated_at
	`, planeID, observedAt, input.Fingerprint, summaryJSON)

	item, err := scanPlaneRuntimeConfigSnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.RuntimeConfigSnapshot{}, ErrPlaneNotFound
		}
		return model.RuntimeConfigSnapshot{}, fmt.Errorf("upsert plane runtime config state: %w", err)
	}
	return item, nil
}

func (s *Store) GetPlaneRuntimeConfigSnapshot(ctx context.Context, planeID string) (*model.RuntimeConfigSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			plane_id,
			observed_at,
			fingerprint,
			summary_json,
			updated_at
		FROM fleet_plane_runtime_config_states
		WHERE plane_id = $1
	`, planeID)

	item, err := scanPlaneRuntimeConfigSnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query plane runtime config state: %w", err)
	}
	return &item, nil
}

func scanPlaneRuntimeConfigSnapshot(scanner interface{ Scan(dest ...any) error }) (model.RuntimeConfigSnapshot, error) {
	var item model.RuntimeConfigSnapshot
	var summaryJSON []byte
	if err := scanner.Scan(
		&item.PlaneID,
		&item.ObservedAt,
		&item.Fingerprint,
		&summaryJSON,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeConfigSnapshot{}, err
	}
	if err := unmarshalJSON(summaryJSON, &item.Summary, map[string]any{}); err != nil {
		return model.RuntimeConfigSnapshot{}, fmt.Errorf("unmarshal plane runtime config summary: %w", err)
	}
	return item, nil
}
