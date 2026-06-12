package store

import (
	"context"
	"fmt"
	"time"
)

const recentControlEventsLimit = 40

type ControlEvent struct {
	ID        string
	Action    string
	Message   string
	CreatedAt time.Time
}

type CreateControlEventInput struct {
	Action  string
	Message string
}

func (s *Store) CreateControlEvent(ctx context.Context, input CreateControlEventInput) error {
	id, err := newID("evt")
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO control_events (
			id,
			action,
			message
		)
		VALUES ($1, $2, $3)
	`,
		id,
		input.Action,
		input.Message,
	); err != nil {
		return fmt.Errorf("insert control event: %w", err)
	}
	return nil
}

func (s *Store) ListRecentControlEvents(ctx context.Context) ([]ControlEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			action,
			message,
			created_at
		FROM control_events
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, recentControlEventsLimit)
	if err != nil {
		return nil, fmt.Errorf("query control events: %w", err)
	}
	defer rows.Close()

	items := make([]ControlEvent, 0)
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

func scanControlEvent(scanner interface{ Scan(dest ...any) error }) (ControlEvent, error) {
	var item ControlEvent
	if err := scanner.Scan(
		&item.ID,
		&item.Action,
		&item.Message,
		&item.CreatedAt,
	); err != nil {
		return ControlEvent{}, fmt.Errorf("scan control event: %w", err)
	}
	return item, nil
}
