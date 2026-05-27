package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/common/project"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrProjectNameAlreadyExists = errors.New("project name already exists")

func (s *Store) CreateProject(ctx context.Context, input project.CreateProjectInput) (project.Project, error) {
	if err := input.Validate(); err != nil {
		return project.Project{}, err
	}

	id, err := newID("prj")
	if err != nil {
		return project.Project{}, err
	}

	quota, err := input.ResolveQuota()
	if err != nil {
		return project.Project{}, err
	}

	created, err := scanProject(s.db.QueryRowContext(ctx, `
		INSERT INTO projects (
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
	`, id, input.Name, input.DisplayName, input.OwnerUserID, quota.MaxServices, quota.CPUMilli, quota.MemoryMi))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return project.Project{}, ErrProjectNameAlreadyExists
		}
		return project.Project{}, fmt.Errorf("insert project: %w", err)
	}

	return created, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]project.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
		FROM projects
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer closeRows(rows)

	items := make([]project.Project, 0)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}

	return items, nil
}

func (s *Store) GetProject(ctx context.Context, projectID string) (project.Project, error) {
	item, err := scanProject(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
		FROM projects
		WHERE id = $1
	`, projectID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("query project: %w", err)
	}

	return item, nil
}

func (s *Store) GetProjectByName(ctx context.Context, name string) (project.Project, error) {
	item, err := scanProject(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
		FROM projects
		WHERE name = $1
	`, name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("query project by name: %w", err)
	}

	return item, nil
}

func (s *Store) UpdateProject(ctx context.Context, projectID string, input project.UpdateProjectInput) (project.Project, error) {
	if err := input.Validate(); err != nil {
		return project.Project{}, err
	}

	quota, err := input.ResolveQuota()
	if err != nil {
		return project.Project{}, err
	}

	updated, err := scanProject(s.db.QueryRowContext(ctx, `
		UPDATE projects
		SET
			display_name = $2,
			quota_max_services = $3,
			quota_cpu_milli = $4,
			quota_memory_mi = $5
		WHERE id = $1
		RETURNING
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
	`, projectID, input.DisplayName, quota.MaxServices, quota.CPUMilli, quota.MemoryMi))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("update project: %w", err)
	}

	return updated, nil
}

func (s *Store) TransferProjectOwnership(ctx context.Context, projectID string, ownerUserID string) (project.Project, error) {
	if strings.TrimSpace(ownerUserID) == "" {
		return project.Project{}, project.ErrOwnerUserIDRequired
	}

	updated, err := scanProject(s.db.QueryRowContext(ctx, `
		UPDATE projects
		SET owner_user_id = $2
		WHERE id = $1
		RETURNING
			id,
			name,
			display_name,
			owner_user_id,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			created_at
	`, projectID, strings.TrimSpace(ownerUserID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("transfer project ownership: %w", err)
	}

	return updated, nil
}

func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func scanProject(scanner interface{ Scan(dest ...any) error }) (project.Project, error) {
	var item project.Project
	err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.DisplayName,
		&item.OwnerUserID,
		&item.Quota.MaxServices,
		&item.Quota.CPUMilli,
		&item.Quota.MemoryMi,
		&item.CreatedAt,
	)
	return item, err
}
