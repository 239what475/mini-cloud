package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
)

var (
	ErrPlaneNotFound             = errors.New("plane not found")
	ErrPlaneNameAlreadyExists    = errors.New("plane name already exists")
	errPlaneNameRequired         = errors.New("name is required")
	errInvalidPlaneName          = errors.New("name must use lowercase letters, digits, and hyphens")
	errPlaneDisplayNameRequired  = errors.New("displayName is required")
	errPlaneProviderRequired     = errors.New("provider is required")
	errPlaneRegionRequired       = errors.New("region is required")
	errPlaneGRPCEndpointRequired = errors.New("grpcEndpoint is required")
	errInvalidPlaneGRPCEndpoint  = errors.New("grpcEndpoint must be a gRPC target such as host:port, grpc://host:port, grpcs://host:port, dns:///name:port, or unix:///path")
	errInvalidPlaneStatus        = errors.New("status must be one of syncing, ready, degraded, offline")
	planeNamePattern             = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type RegisterPlaneInput struct {
	Name         string
	DisplayName  string
	Provider     string
	Region       string
	GRPCEndpoint string
}

type UpdatePlaneStatusInput struct {
	Status          string
	Message         string
	LastHeartbeatAt *time.Time
	LastSyncAt      *time.Time
}

func (in RegisterPlaneInput) validate() error {
	return validatePlaneFields(in.Name, in.DisplayName, in.Provider, in.Region, in.GRPCEndpoint)
}

func validatePlaneFields(name string, displayName string, provider string, region string, grpcEndpoint string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return invalidInput(errPlaneNameRequired)
	case !planeNamePattern.MatchString(strings.TrimSpace(name)):
		return invalidInput(errInvalidPlaneName)
	case strings.TrimSpace(displayName) == "":
		return invalidInput(errPlaneDisplayNameRequired)
	case strings.TrimSpace(provider) == "":
		return invalidInput(errPlaneProviderRequired)
	case strings.TrimSpace(region) == "":
		return invalidInput(errPlaneRegionRequired)
	}
	_, err := normalizeGRPCEndpoint(grpcEndpoint)
	return invalidInput(err)
}

func (in UpdatePlaneStatusInput) validate() error {
	if !model.IsPlaneStatus(in.Status) {
		return invalidInput(errInvalidPlaneStatus)
	}
	return nil
}

func (s *Store) RegisterPlane(ctx context.Context, input RegisterPlaneInput) (model.PlaneDetail, error) {
	if err := input.validate(); err != nil {
		return model.PlaneDetail{}, err
	}
	grpcEndpoint, err := normalizeGRPCEndpoint(input.GRPCEndpoint)
	if err != nil {
		return model.PlaneDetail{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.PlaneDetail{}, fmt.Errorf("begin register plane tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var planeID string
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM planes
		WHERE name = $1
	`, strings.TrimSpace(input.Name)).Scan(&planeID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return model.PlaneDetail{}, fmt.Errorf("query existing plane: %w", err)
		}
		planeID, err = newID("pln")
		if err != nil {
			return model.PlaneDetail{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO planes (
				id,
				name,
				display_name,
				provider,
				region,
				grpc_endpoint
			)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, planeID, strings.TrimSpace(input.Name), strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Provider), strings.TrimSpace(input.Region), grpcEndpoint); err != nil {
			return model.PlaneDetail{}, fmt.Errorf("insert plane: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO plane_statuses (
				plane_id,
				status,
				message,
				last_heartbeat_at
			)
			VALUES ($1, $2, $3, now())
		`, planeID, model.StatusSyncing, "cloud-plane registered; awaiting first sync"); err != nil {
			return model.PlaneDetail{}, fmt.Errorf("insert plane status: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE planes
			SET
				display_name = $2,
				provider = $3,
				region = $4,
				grpc_endpoint = $5
			WHERE id = $1
		`, planeID, strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Provider), strings.TrimSpace(input.Region), grpcEndpoint); err != nil {
			return model.PlaneDetail{}, fmt.Errorf("update plane: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE plane_statuses
			SET
				status = $2,
				message = $3,
				last_heartbeat_at = now(),
				updated_at = now()
			WHERE plane_id = $1
		`, planeID, model.StatusSyncing, "cloud-plane registered; awaiting first sync"); err != nil {
			return model.PlaneDetail{}, fmt.Errorf("update plane status: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.PlaneDetail{}, fmt.Errorf("commit register plane: %w", err)
	}
	return s.GetPlane(ctx, planeID)
}

func (s *Store) ListPlanes(ctx context.Context) ([]model.PlaneDetail, error) {
	rows, err := s.db.QueryContext(ctx, planeDetailBaseQuery(`
		ORDER BY p.created_at ASC, p.id ASC
	`))
	if err != nil {
		return nil, fmt.Errorf("query control planes: %w", err)
	}
	defer rows.Close()

	items := make([]model.PlaneDetail, 0)
	for rows.Next() {
		item, err := scanPlaneDetail(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate control planes: %w", err)
	}
	return items, nil
}

func (s *Store) GetPlane(ctx context.Context, planeID string) (model.PlaneDetail, error) {
	row := s.db.QueryRowContext(ctx, planeDetailBaseQuery(`
		WHERE p.id = $1
	`), planeID)

	item, err := scanPlaneDetail(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PlaneDetail{}, ErrPlaneNotFound
		}
		return model.PlaneDetail{}, fmt.Errorf("query plane: %w", err)
	}
	return item, nil
}

func (s *Store) DeletePlane(ctx context.Context, planeID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM planes WHERE id = $1`, planeID)
	if err != nil {
		return fmt.Errorf("delete plane: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrPlaneNotFound
	}
	return nil
}

func (s *Store) ListPlaneIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM planes
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query plane ids: %w", err)
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var planeID string
		if err := rows.Scan(&planeID); err != nil {
			return nil, fmt.Errorf("scan plane id: %w", err)
		}
		items = append(items, planeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plane ids: %w", err)
	}
	return items, nil
}

func (s *Store) UpdatePlaneStatus(ctx context.Context, planeID string, input UpdatePlaneStatusInput) error {
	if err := input.validate(); err != nil {
		return err
	}
	var lastHeartbeatAt any
	if input.LastHeartbeatAt != nil && !input.LastHeartbeatAt.IsZero() {
		lastHeartbeatAt = input.LastHeartbeatAt.UTC()
	}
	var lastSyncAt any
	if input.LastSyncAt != nil && !input.LastSyncAt.IsZero() {
		lastSyncAt = input.LastSyncAt.UTC()
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE plane_statuses
		SET
			status = $2,
			message = $3,
			last_heartbeat_at = $4,
			last_sync_at = $5,
			updated_at = now()
		WHERE plane_id = $1
	`, planeID, input.Status, input.Message, lastHeartbeatAt, lastSyncAt)
	if err != nil {
		return fmt.Errorf("update plane status: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrPlaneNotFound
	}
	return nil
}

func planeDetailBaseQuery(suffix string) string {
	return `
		SELECT
			p.id,
			p.name,
			p.display_name,
			p.provider,
			p.region,
			p.grpc_endpoint,
			p.created_at,
			s.plane_id,
			s.status,
			s.message,
			s.last_heartbeat_at,
			s.last_sync_at,
			s.updated_at,
			ris.plane_id,
			ris.observed_at,
			ris.nodes_total,
			ris.nodes_ready,
			ris.cpu_milli_capacity,
			ris.cpu_milli_allocated,
			ris.memory_mi_capacity,
			ris.memory_mi_allocated,
			ris.updated_at
		FROM planes p
		JOIN plane_statuses s
			ON s.plane_id = p.id
		LEFT JOIN plane_node_inventory_states ris
			ON ris.plane_id = p.id
	` + suffix
}

func scanPlaneDetail(scanner interface{ Scan(dest ...any) error }) (model.PlaneDetail, error) {
	var item model.PlaneDetail
	var heartbeat sql.NullTime
	var syncAt sql.NullTime
	var inventoryPlaneID sql.NullString
	var inventoryObservedAt sql.NullTime
	var inventoryNodesTotal sql.NullInt64
	var inventoryNodesReady sql.NullInt64
	var inventoryCPUMilliCapacity sql.NullInt64
	var inventoryCPUMilliAllocated sql.NullInt64
	var inventoryMemoryMiCapacity sql.NullInt64
	var inventoryMemoryMiAllocated sql.NullInt64
	var inventoryUpdatedAt sql.NullTime

	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.DisplayName,
		&item.Provider,
		&item.Region,
		&item.GRPCEndpoint,
		&item.CreatedAt,
		&item.Status.PlaneID,
		&item.Status.Status,
		&item.Status.Message,
		&heartbeat,
		&syncAt,
		&item.Status.UpdatedAt,
		&inventoryPlaneID,
		&inventoryObservedAt,
		&inventoryNodesTotal,
		&inventoryNodesReady,
		&inventoryCPUMilliCapacity,
		&inventoryCPUMilliAllocated,
		&inventoryMemoryMiCapacity,
		&inventoryMemoryMiAllocated,
		&inventoryUpdatedAt,
	); err != nil {
		return model.PlaneDetail{}, err
	}
	if heartbeat.Valid {
		value := heartbeat.Time
		item.Status.LastHeartbeatAt = &value
	}
	if syncAt.Valid {
		value := syncAt.Time
		item.Status.LastSyncAt = &value
	}
	if inventoryPlaneID.Valid {
		item.LatestNodeInventory = &model.NodeInventorySnapshot{
			PlaneID:           inventoryPlaneID.String,
			ObservedAt:        inventoryObservedAt.Time,
			NodesTotal:        int(inventoryNodesTotal.Int64),
			NodesReady:        int(inventoryNodesReady.Int64),
			CPUMilliCapacity:  int(inventoryCPUMilliCapacity.Int64),
			CPUMilliAllocated: int(inventoryCPUMilliAllocated.Int64),
			MemoryMiCapacity:  int(inventoryMemoryMiCapacity.Int64),
			MemoryMiAllocated: int(inventoryMemoryMiAllocated.Int64),
			UpdatedAt:         inventoryUpdatedAt.Time,
		}
	}
	return item, nil
}

func normalizeGRPCEndpoint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errPlaneGRPCEndpointRequired
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", errInvalidPlaneGRPCEndpoint
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", errInvalidPlaneGRPCEndpoint
	}
	if strings.HasPrefix(lower, "grpc://") {
		target := strings.TrimSpace(value[len("grpc://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", errInvalidPlaneGRPCEndpoint
		}
		return target, nil
	}
	if strings.HasPrefix(lower, "grpcs://") || strings.HasPrefix(lower, "dns:///") || strings.HasPrefix(lower, "unix:///") {
		return value, nil
	}
	return value, nil
}
