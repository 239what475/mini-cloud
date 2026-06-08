package store

import (
	"context"
	"fmt"

	"mini-cloud/internal/controlplane/eventlog"
)

const recentControlEventsLimit = 40

func (s *Store) CreateControlEvent(ctx context.Context, input eventlog.CreateInput) (eventlog.Record, error) {
	id, err := newID("evt")
	if err != nil {
		return eventlog.Record{}, err
	}

	var created eventlog.Record
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO control_events (
			id,
			action,
			target_type,
			target_id,
			target_name
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING
			id,
			action,
			target_type,
			target_id,
			target_name,
			created_at
	`,
		id,
		input.Action,
		input.TargetType,
		input.TargetID,
		input.TargetName,
	).Scan(
		&created.ID,
		&created.Action,
		&created.TargetType,
		&created.TargetID,
		&created.TargetName,
		&created.CreatedAt,
	)
	if err != nil {
		return eventlog.Record{}, fmt.Errorf("insert control event: %w", err)
	}
	return created, nil
}

func (s *Store) ListRecentControlEvents(ctx context.Context) ([]eventlog.Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			action,
			target_type,
			target_id,
			target_name,
			created_at
		FROM control_events
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, recentControlEventsLimit)
	if err != nil {
		return nil, fmt.Errorf("query control events: %w", err)
	}
	defer closeRows(rows)

	items := make([]eventlog.Record, 0)
	for rows.Next() {
		item, err := scanControlEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control events: %w", err)
	}
	return items, nil
}

func scanControlEvent(scanner interface{ Scan(dest ...any) error }) (eventlog.Record, error) {
	var item eventlog.Record
	if err := scanner.Scan(
		&item.ID,
		&item.Action,
		&item.TargetType,
		&item.TargetID,
		&item.TargetName,
		&item.CreatedAt,
	); err != nil {
		return eventlog.Record{}, fmt.Errorf("scan control event: %w", err)
	}
	return item, nil
}
