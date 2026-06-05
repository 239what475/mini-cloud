package store

import (
	"context"
	"fmt"

	"mini-cloud/internal/common/operationhistory"
)

func (s *Store) CreateOperationEvent(ctx context.Context, input operationhistory.CreateInput) (operationhistory.Record, error) {
	id, err := newID("ope")
	if err != nil {
		return operationhistory.Record{}, err
	}

	var created operationhistory.Record
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO operation_events (
			id,
			action,
			target_type,
			target_id,
			target_name,
			result
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			action,
			target_type,
			target_id,
			target_name,
			result,
			created_at
	`,
		id,
		input.Action,
		input.TargetType,
		input.TargetID,
		input.TargetName,
		input.Result,
	).Scan(
		&created.ID,
		&created.Action,
		&created.TargetType,
		&created.TargetID,
		&created.TargetName,
		&created.Result,
		&created.CreatedAt,
	)
	if err != nil {
		return operationhistory.Record{}, fmt.Errorf("insert operation event: %w", err)
	}
	return created, nil
}

func (s *Store) ListControlOperationEvents(ctx context.Context, limit int) ([]operationhistory.Record, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			action,
			target_type,
			target_id,
			target_name,
			result,
			created_at
		FROM operation_events
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query operation events: %w", err)
	}
	defer closeRows(rows)

	items := make([]operationhistory.Record, 0)
	for rows.Next() {
		item, err := scanOperationEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operation events: %w", err)
	}
	return items, nil
}

func scanOperationEvent(scanner interface{ Scan(dest ...any) error }) (operationhistory.Record, error) {
	var item operationhistory.Record
	if err := scanner.Scan(
		&item.ID,
		&item.Action,
		&item.TargetType,
		&item.TargetID,
		&item.TargetName,
		&item.Result,
		&item.CreatedAt,
	); err != nil {
		return operationhistory.Record{}, fmt.Errorf("scan operation event: %w", err)
	}
	return item, nil
}
