package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
)

func (s *Store) RecordPlaneRuntimeConfig(ctx context.Context, planeID string, input plane.RecordRuntimeConfigInput) (plane.RuntimeConfigSnapshot, error) {
	observedAt := input.ResolvedObservedAt(time.Now().UTC())
	summaryJSON, err := marshalJSON(input.Summary, map[string]any{})
	if err != nil {
		return plane.RuntimeConfigSnapshot{}, fmt.Errorf("marshal plane runtime config summary: %w", err)
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
			return plane.RuntimeConfigSnapshot{}, ErrPlaneNotFound
		}
		return plane.RuntimeConfigSnapshot{}, fmt.Errorf("upsert plane runtime config state: %w", err)
	}
	return item, nil
}

func (s *Store) GetPlaneRuntimeConfigSnapshot(ctx context.Context, planeID string) (*plane.RuntimeConfigSnapshot, error) {
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

func scanPlaneRuntimeConfigSnapshot(scanner interface{ Scan(dest ...any) error }) (plane.RuntimeConfigSnapshot, error) {
	var item plane.RuntimeConfigSnapshot
	var summaryJSON []byte
	if err := scanner.Scan(
		&item.PlaneID,
		&item.ObservedAt,
		&item.Fingerprint,
		&summaryJSON,
		&item.UpdatedAt,
	); err != nil {
		return plane.RuntimeConfigSnapshot{}, err
	}
	if err := unmarshalJSON(summaryJSON, &item.Summary, map[string]any{}); err != nil {
		return plane.RuntimeConfigSnapshot{}, fmt.Errorf("unmarshal plane runtime config summary: %w", err)
	}
	return item, nil
}
