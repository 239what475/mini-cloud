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

	detailsJSON, err := marshalJSON(input.Details, map[string]any{})
	if err != nil {
		return operationhistory.Record{}, fmt.Errorf("marshal operation event details: %w", err)
	}

	var created operationhistory.Record
	var rawDetails []byte
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO operation_events (
			id,
			project_id,
			action,
			target_type,
			target_id,
			target_name,
			actor_kind,
			actor_id,
			actor_label,
			actor_project_id,
			request_method,
			request_path,
			result,
			details_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING
			id,
			project_id,
			action,
			target_type,
			target_id,
			target_name,
			actor_kind,
			actor_id,
			actor_label,
			actor_project_id,
			request_method,
			request_path,
			result,
			details_json,
			created_at
	`,
		id,
		input.ProjectID,
		input.Action,
		input.TargetType,
		input.TargetID,
		input.TargetName,
		input.ActorKind,
		input.ActorID,
		input.ActorLabel,
		input.ActorProjectID,
		input.RequestMethod,
		input.RequestPath,
		input.Result,
		detailsJSON,
	).Scan(
		&created.ID,
		&created.ProjectID,
		&created.Action,
		&created.TargetType,
		&created.TargetID,
		&created.TargetName,
		&created.ActorKind,
		&created.ActorID,
		&created.ActorLabel,
		&created.ActorProjectID,
		&created.RequestMethod,
		&created.RequestPath,
		&created.Result,
		&rawDetails,
		&created.CreatedAt,
	)
	if err != nil {
		return operationhistory.Record{}, fmt.Errorf("insert operation event: %w", err)
	}
	if err := unmarshalJSON(rawDetails, &created.Details, map[string]any{}); err != nil {
		return operationhistory.Record{}, fmt.Errorf("decode operation event details: %w", err)
	}
	return created, nil
}

func (s *Store) ListPlatformOperationEvents(ctx context.Context, limit int) ([]operationhistory.Record, error) {
	return s.listOperationEvents(ctx, "", limit)
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
			project_id,
			action,
			target_type,
			target_id,
			target_name,
			actor_kind,
			actor_id,
			actor_label,
			actor_project_id,
			request_method,
			request_path,
			result,
			details_json,
			created_at
		FROM operation_events
		WHERE action LIKE 'control.%'
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query control operation events: %w", err)
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
		return nil, fmt.Errorf("iterate control operation events: %w", err)
	}
	return items, nil
}

func (s *Store) ListProjectOperationEvents(ctx context.Context, projectID string, limit int) ([]operationhistory.Record, error) {
	return s.listOperationEvents(ctx, projectID, limit)
}

func (s *Store) listOperationEvents(ctx context.Context, projectID string, limit int) ([]operationhistory.Record, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}

	query := `
		SELECT
			id,
			project_id,
			action,
			target_type,
			target_id,
			target_name,
			actor_kind,
			actor_id,
			actor_label,
			actor_project_id,
			request_method,
			request_path,
			result,
			details_json,
			created_at
		FROM operation_events
	`
	args := []any{limit}
	if projectID != "" {
		query += `
		WHERE project_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
		`
		args = []any{projectID, limit}
	} else {
		query += `
		ORDER BY created_at DESC, id DESC
		LIMIT $1
		`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
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
	var rawDetails []byte
	if err := scanner.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Action,
		&item.TargetType,
		&item.TargetID,
		&item.TargetName,
		&item.ActorKind,
		&item.ActorID,
		&item.ActorLabel,
		&item.ActorProjectID,
		&item.RequestMethod,
		&item.RequestPath,
		&item.Result,
		&rawDetails,
		&item.CreatedAt,
	); err != nil {
		return operationhistory.Record{}, fmt.Errorf("scan operation event: %w", err)
	}
	if err := unmarshalJSON(rawDetails, &item.Details, map[string]any{}); err != nil {
		return operationhistory.Record{}, fmt.Errorf("decode operation event details: %w", err)
	}
	return item, nil
}
