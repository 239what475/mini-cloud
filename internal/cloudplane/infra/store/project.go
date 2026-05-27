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

// ErrProjectNameAlreadyExists 表示项目名称已被占用。
var ErrProjectNameAlreadyExists = errors.New("project name already exists")

// CreateProject 创建项目并写入配额与 owner。
// 参数说明：ctx 控制数据库请求生命周期；input 是项目创建参数。
func (s *Store) CreateProject(ctx context.Context, input project.CreateProjectInput) (project.Project, error) {
	// 先校验名称、展示名、owner 和配额输入。
	if err := input.Validate(); err != nil {
		return project.Project{}, err
	}

	// southbound apply 会传入 control-plane project ID；本地测试或内部调用未指定时再生成本地 ID。
	id := strings.TrimSpace(input.ID)
	if id == "" {
		generatedID, err := newID("prj")
		if err != nil {
			return project.Project{}, err
		}
		id = generatedID
	}

	// ResolveQuota 会填充默认配额并校验配额范围。
	quota, err := input.ResolveQuota()
	if err != nil {
		return project.Project{}, err
	}

	// 插入项目并返回数据库最终值。
	var created project.Project
	err = scanProjectRow(s.db.QueryRowContext(ctx, `
		INSERT INTO projects (
			id,
			name,
			display_name,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			owner_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING
			id,
			name,
			display_name,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			owner_user_id,
			created_at
	`, id, input.Name, input.DisplayName, quota.MaxServices, quota.CPUMilli, quota.MemoryMi, input.OwnerUserID), &created)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			// name 唯一约束冲突转换为领域错误。
			return project.Project{}, ErrProjectNameAlreadyExists
		}
		return project.Project{}, fmt.Errorf("insert project: %w", err)
	}

	return created, nil
}

// GetProject 按项目 ID 查询项目。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识。
func (s *Store) GetProject(ctx context.Context, projectID string) (project.Project, error) {
	// 按主键读取项目完整字段。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			display_name,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			owner_user_id,
			created_at
		FROM projects
		WHERE id = $1
	`, projectID)

	var item project.Project
	if err := scanProjectRow(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为项目 not found。
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("query project: %w", err)
	}

	return item, nil
}

// GetProjectByName 按项目名称查询项目。
// 参数说明：ctx 控制数据库请求生命周期；name 表示项目名称。
func (s *Store) GetProjectByName(ctx context.Context, name string) (project.Project, error) {
	// name 有唯一约束，最多返回一条记录。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			display_name,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			owner_user_id,
			created_at
		FROM projects
		WHERE name = $1
	`, name)

	var item project.Project
	if err := scanProjectRow(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为项目 not found。
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("query project by name: %w", err)
	}

	return item, nil
}

// UpdateProject 更新项目展示名和配额。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识；input 是项目更新参数。
func (s *Store) UpdateProject(ctx context.Context, projectID string, input project.UpdateProjectInput) (project.Project, error) {
	// 先校验更新输入。
	if err := input.Validate(); err != nil {
		return project.Project{}, err
	}

	// ResolveQuota 会填充默认配额并校验配额范围。
	quota, err := input.ResolveQuota()
	if err != nil {
		return project.Project{}, err
	}

	// 更新可变字段并返回项目完整快照。
	var updated project.Project
	err = scanProjectRow(s.db.QueryRowContext(ctx, `
		UPDATE projects
		SET
			display_name = $2,
			quota_max_services = $3,
			quota_cpu_milli = $4,
			quota_memory_mi = $5,
			owner_user_id = $6
		WHERE id = $1
		RETURNING
			id,
			name,
			display_name,
			quota_max_services,
			quota_cpu_milli,
			quota_memory_mi,
			owner_user_id,
			created_at
	`, projectID, input.DisplayName, quota.MaxServices, quota.CPUMilli, quota.MemoryMi, input.OwnerUserID), &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为项目 not found。
			return project.Project{}, ErrProjectNotFound
		}
		return project.Project{}, fmt.Errorf("update project: %w", err)
	}

	return updated, nil
}

// scanProjectRow 从 SQL 扫描器读取一行数据并组装领域对象。
// 参数说明：scanner 是当前数据库查询结果行扫描器；item 是当前处理的单条领域对象。
func scanProjectRow(scanner interface{ Scan(dest ...any) error }, item *project.Project) error {
	// 扫描顺序必须和 project SELECT/RETURNING 字段顺序一致。
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.DisplayName,
		&item.Quota.MaxServices,
		&item.Quota.CPUMilli,
		&item.Quota.MemoryMi,
		&item.OwnerUserID,
		&item.CreatedAt,
	); err != nil {
		// scan 错误原样返回，让调用方区分 sql.ErrNoRows 和其他扫描错误。
		return err
	}
	// owner 和 quota 已直接写入 item；本 helper 不做额外领域校验。
	return nil
}
