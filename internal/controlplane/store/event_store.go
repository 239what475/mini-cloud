package store

import (
	"context"
	"fmt"
	"time"
)

const recentControlEventsLimit = 40

type ControlEvent struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetID"`
	TargetName string    `json:"targetName"`
	CreatedAt  time.Time `json:"createdAt"`
}

type CreateControlEventInput struct {
	Action     string
	TargetType string
	TargetID   string
	TargetName string
}

func (s *Store) CreateControlEvent(ctx context.Context, input CreateControlEventInput) (ControlEvent, error) {
	id, err := newID("evt")
	if err != nil {
		return ControlEvent{}, err
	}

	var created ControlEvent
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
		return ControlEvent{}, fmt.Errorf("insert control event: %w", err)
	}
	return created, nil
}

func (s *Store) ListRecentControlEvents(ctx context.Context) ([]ControlEvent, error) {
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
		&item.TargetType,
		&item.TargetID,
		&item.TargetName,
		&item.CreatedAt,
	); err != nil {
		return ControlEvent{}, fmt.Errorf("scan control event: %w", err)
	}
	return item, nil
}
