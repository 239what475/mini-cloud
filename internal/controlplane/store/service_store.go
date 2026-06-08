package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/common/projectedfile"
	controlservice "mini-cloud/internal/controlplane/service"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceNotFound           = errors.New("service not found")
	ErrServiceNameAlreadyExists  = errors.New("service name already exists")
	ErrServiceGenerationConflict = errors.New("service generation changed before reconcile write could be committed")
)

const serviceSelectColumns = `
	id,
	name,
	display_name,
	spec_plane_id,
	spec_instance_class,
	spec_exposure,
	spec_image,
	spec_command_json,
	spec_args_json,
	spec_default_port,
	spec_readiness_path,
	spec_env_json,
	spec_secret_env_json,
	spec_registry_credential_id,
	spec_files_json,
	status_run_json,
	generation,
	status_desired_state,
	status_observed_generation,
	status_phase,
	status_healthy,
	status_message,
	status_last_reconciled_at,
	status_assigned_plane_id,
	status_remote_status,
	status_remote_message,
	created_at,
	updated_at
`

func (s *Store) CreateService(ctx context.Context, input controlservice.CreateInput) (controlservice.Service, error) {
	if err := input.Validate(); err != nil {
		return controlservice.Service{}, err
	}
	planeID, instanceClass, err := controlservice.ResolveServicePlacementFields(input.Spec.PlaneID, input.Spec.InstanceClass)
	if err != nil {
		return controlservice.Service{}, err
	}
	if err := s.ensureServiceReferencesResolved(ctx, planeID, input.Spec.RegistryCredentialID); err != nil {
		return controlservice.Service{}, err
	}

	id, err := newID("svc")
	if err != nil {
		return controlservice.Service{}, err
	}

	commandJSON, err := marshalJSON(input.Spec.Command, []string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service command: %w", err)
	}
	argsJSON, err := marshalJSON(input.Spec.Args, []string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service args: %w", err)
	}
	envJSON, err := marshalJSON(input.Spec.Env, map[string]string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service env: %w", err)
	}
	secretEnvJSON, err := marshalJSON(input.Spec.SecretEnv, map[string]string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service secret env: %w", err)
	}
	filesJSON, err := marshalJSON(projectedfile.CloneFiles(input.Spec.Files), []projectedfile.File{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service files: %w", err)
	}
	initialStatus := controlservice.PendingStatus(0, "waiting for service reconcile")
	initialRun := controlservice.RunStatus{Phase: controlservice.RunPhasePending}
	runJSON, err := marshalJSON(initialRun, controlservice.RunStatus{Phase: controlservice.RunPhasePending})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service initial run: %w", err)
	}

	item, err := scanService(s.db.QueryRowContext(ctx, `
		INSERT INTO fleet_services (
			id,
			name,
			display_name,
			spec_plane_id,
			spec_instance_class,
			spec_exposure,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_default_port,
			spec_readiness_path,
			spec_env_json,
			spec_secret_env_json,
			spec_registry_credential_id,
			spec_files_json,
			status_run_json,
			generation,
			status_desired_state,
			status_observed_generation,
			status_phase,
			status_healthy,
			status_message,
			status_last_reconciled_at,
			status_assigned_plane_id,
			status_remote_status,
			status_remote_message
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 1, $17, $18, $19, $20, $21, NULL, NULL, '', '')
		RETURNING `+serviceSelectColumns+`
	`,
		id,
		input.Name,
		input.DisplayName,
		planeID,
		instanceClass,
		input.Spec.Exposure,
		input.Spec.Image,
		commandJSON,
		argsJSON,
		input.Spec.DefaultPort,
		input.Spec.ReadinessPath,
		envJSON,
		secretEnvJSON,
		input.Spec.RegistryCredentialID,
		filesJSON,
		runJSON,
		controlservice.DesiredStateActive,
		initialStatus.ObservedGeneration,
		initialStatus.Phase,
		initialStatus.Healthy,
		initialStatus.Message,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				return controlservice.Service{}, ErrServiceNameAlreadyExists
			}
		}
		return controlservice.Service{}, fmt.Errorf("insert service: %w", err)
	}
	return item, nil
}

func (s *Store) ListServices(ctx context.Context) ([]controlservice.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer closeRows(rows)

	items := make([]controlservice.Service, 0)
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate services: %w", err)
	}
	return items, nil
}

func (s *Store) GetService(ctx context.Context, serviceID string) (controlservice.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		WHERE id = $1
	`, serviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Service{}, ErrServiceNotFound
		}
		return controlservice.Service{}, fmt.Errorf("query service: %w", err)
	}
	return item, nil
}

func getServiceForUpdateTx(ctx context.Context, tx *sql.Tx, serviceID string) (controlservice.Service, error) {
	item, err := scanService(tx.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		WHERE id = $1
		FOR UPDATE
	`, serviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Service{}, ErrServiceNotFound
		}
		return controlservice.Service{}, fmt.Errorf("query service for update: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateService(ctx context.Context, serviceID string, input controlservice.UpdateInput) (controlservice.Service, error) {
	commandJSON, err := marshalJSON(input.Spec.Command, []string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service command for update: %w", err)
	}
	argsJSON, err := marshalJSON(input.Spec.Args, []string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service args for update: %w", err)
	}
	envJSON, err := marshalJSON(input.Spec.Env, map[string]string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service env for update: %w", err)
	}
	secretEnvJSON, err := marshalJSON(input.Spec.SecretEnv, map[string]string{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service secret env for update: %w", err)
	}
	filesJSON, err := marshalJSON(projectedfile.CloneFiles(input.Spec.Files), []projectedfile.File{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service files for update: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("begin update service tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := getServiceForUpdateTx(ctx, tx, serviceID)
	if err != nil {
		return controlservice.Service{}, err
	}
	if err := input.Validate(current.Metadata.Name); err != nil {
		return controlservice.Service{}, err
	}
	planeID, instanceClass, err := controlservice.ResolveServicePlacementFields(input.Spec.PlaneID, input.Spec.InstanceClass)
	if err != nil {
		return controlservice.Service{}, err
	}
	if err := s.ensureServiceReferencesResolved(ctx, planeID, input.Spec.RegistryCredentialID); err != nil {
		return controlservice.Service{}, err
	}
	currentRunJSON, err := marshalJSON(current.Status.Run, controlservice.RunStatus{Phase: controlservice.RunPhasePending})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service run for update: %w", err)
	}

	nextGeneration := current.Metadata.Generation + 1
	pendingStatus := controlservice.PendingStatus(current.Status.Observed.ObservedGeneration, "waiting for service reconcile")

	item, err := scanService(tx.QueryRowContext(ctx, `
		UPDATE fleet_services
		SET
			display_name = $2,
			spec_plane_id = $3,
			spec_instance_class = $4,
			spec_exposure = $5,
			spec_image = $6,
			spec_command_json = $7,
			spec_args_json = $8,
			spec_default_port = $9,
			spec_readiness_path = $10,
			spec_env_json = $11,
			spec_secret_env_json = $12,
			spec_registry_credential_id = $13,
			spec_files_json = $14,
			status_run_json = $15,
			generation = $16,
			status_desired_state = $17,
			status_observed_generation = $18,
			status_phase = $19,
			status_healthy = $20,
			status_message = $21,
			status_last_reconciled_at = NULL,
			status_remote_status = '',
			status_remote_message = '',
			updated_at = now()
		WHERE id = $1
			AND generation = $22
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		input.DisplayName,
		planeID,
		instanceClass,
		input.Spec.Exposure,
		input.Spec.Image,
		commandJSON,
		argsJSON,
		input.Spec.DefaultPort,
		input.Spec.ReadinessPath,
		envJSON,
		secretEnvJSON,
		input.Spec.RegistryCredentialID,
		filesJSON,
		currentRunJSON,
		nextGeneration,
		controlservice.DesiredStateActive,
		pendingStatus.ObservedGeneration,
		pendingStatus.Phase,
		pendingStatus.Healthy,
		pendingStatus.Message,
		current.Metadata.Generation,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return controlservice.Service{}, fmt.Errorf("update service: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return controlservice.Service{}, fmt.Errorf("commit update service: %w", err)
	}
	return item, nil
}

func (s *Store) MarkServiceDeletionRequested(ctx context.Context, serviceID string) (controlservice.Service, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("begin delete service tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := getServiceForUpdateTx(ctx, tx, serviceID)
	if err != nil {
		return controlservice.Service{}, err
	}
	if current.Status.DesiredState == controlservice.DesiredStateDeleted {
		return current, nil
	}

	nextGeneration := current.Metadata.Generation + 1
	deletingStatus := controlservice.DeletingStatus(current.Status.Observed.ObservedGeneration, "waiting for remote service teardown")
	currentRunJSON, err := marshalJSON(current.Status.Run, controlservice.RunStatus{Phase: controlservice.RunPhasePending})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service run for delete: %w", err)
	}

	item, err := scanService(tx.QueryRowContext(ctx, `
		UPDATE fleet_services
		SET
			generation = $2,
			status_desired_state = $3,
			status_observed_generation = $4,
			status_phase = $5,
			status_healthy = $6,
			status_message = $7,
			status_run_json = $8,
			status_last_reconciled_at = NULL,
			status_remote_status = 'deleting',
			status_remote_message = 'waiting for remote service teardown',
			updated_at = now()
		WHERE id = $1
			AND generation = $9
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		nextGeneration,
		controlservice.DesiredStateDeleted,
		deletingStatus.ObservedGeneration,
		deletingStatus.Phase,
		deletingStatus.Healthy,
		deletingStatus.Message,
		currentRunJSON,
		current.Metadata.Generation,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return controlservice.Service{}, fmt.Errorf("mark service deletion requested: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return controlservice.Service{}, fmt.Errorf("commit delete service: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateServiceStatus(ctx context.Context, serviceID string, input controlservice.UpdateStatusInput) (controlservice.Service, error) {
	return s.updateServiceStatus(ctx, serviceID, nil, input)
}

func (s *Store) UpdateServiceStatusForGeneration(ctx context.Context, serviceID string, expectedGeneration int64, input controlservice.UpdateStatusInput) (controlservice.Service, error) {
	return s.updateServiceStatus(ctx, serviceID, &expectedGeneration, input)
}

func (s *Store) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration *int64, input controlservice.UpdateStatusInput) (controlservice.Service, error) {
	current, err := s.GetService(ctx, serviceID)
	if err != nil {
		return controlservice.Service{}, err
	}
	if expectedGeneration != nil && current.Metadata.Generation != *expectedGeneration {
		return controlservice.Service{}, ErrServiceGenerationConflict
	}

	nextRun := current.Status.Run
	if input.Run != nil {
		nextRun = controlservice.CloneRunStatus(*input.Run)
	}
	runJSON, err := marshalJSON(nextRun, controlservice.RunStatus{Phase: controlservice.RunPhasePending})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service run status: %w", err)
	}

	query := `
		UPDATE fleet_services
		SET
			status_observed_generation = $2,
			status_phase = $3,
			status_healthy = $4,
			status_message = $5,
			status_last_reconciled_at = $6,
			status_run_json = $7,
			status_assigned_plane_id = COALESCE($8, status_assigned_plane_id),
			status_remote_status = COALESCE($9, status_remote_status),
			status_remote_message = COALESCE($10, status_remote_message),
			updated_at = now()
		WHERE id = $1
	`
	args := []any{
		serviceID,
		input.ObservedGeneration,
		input.Phase,
		input.Healthy,
		input.Message,
		input.LastReconciledAt,
		runJSON,
		nullableOptionalString(input.AssignedPlaneID),
		nullableOptionalString(input.RemoteStatus),
		nullableOptionalString(input.RemoteMessage),
	}
	if expectedGeneration != nil {
		query += ` AND generation = $11`
		args = append(args, *expectedGeneration)
	}
	query += ` RETURNING ` + serviceSelectColumns

	item, err := scanService(s.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if expectedGeneration != nil {
				return controlservice.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, *expectedGeneration)
			}
			return controlservice.Service{}, ErrServiceNotFound
		}
		return controlservice.Service{}, fmt.Errorf("update service status: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteService(ctx context.Context, serviceID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_services WHERE id = $1`, serviceID)
	if err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func (s *Store) DeleteServiceForGeneration(ctx context.Context, serviceID string, expectedGeneration int64) error {
	row := s.db.QueryRowContext(ctx, `
		DELETE FROM fleet_services
		WHERE id = $1
			AND generation = $2
		RETURNING id
	`, serviceID, expectedGeneration)
	var deletedID string
	if err := row.Scan(&deletedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return classifyServiceGenerationConflict(ctx, s, serviceID, expectedGeneration)
		}
		return fmt.Errorf("delete service for generation: %w", err)
	}
	return nil
}

func classifyServiceGenerationConflict(ctx context.Context, stores *Store, serviceID string, expectedGeneration int64) error {
	current, err := stores.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	if current.Metadata.Generation != expectedGeneration {
		return ErrServiceGenerationConflict
	}
	return ErrServiceNotFound
}

func scanService(scanner interface{ Scan(dest ...any) error }) (controlservice.Service, error) {
	var item controlservice.Service
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var secretEnvJSON []byte
	var filesJSON []byte
	var runJSON []byte
	var lastReconciledAt sql.NullTime
	var assignedPlaneID sql.NullString
	if err := scanner.Scan(
		&item.Metadata.ID,
		&item.Metadata.Name,
		&item.Metadata.DisplayName,
		&item.Spec.PlaneID,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&item.Spec.DefaultPort,
		&item.Spec.ReadinessPath,
		&envJSON,
		&secretEnvJSON,
		&item.Spec.RegistryCredentialID,
		&filesJSON,
		&runJSON,
		&item.Metadata.Generation,
		&item.Status.DesiredState,
		&item.Status.Observed.ObservedGeneration,
		&item.Status.Observed.Phase,
		&item.Status.Observed.Healthy,
		&item.Status.Observed.Message,
		&lastReconciledAt,
		&assignedPlaneID,
		&item.Status.Observed.RemoteStatus,
		&item.Status.Observed.RemoteMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return controlservice.Service{}, err
	}
	if err := unmarshalJSON(commandJSON, &item.Spec.Command, []string{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &item.Spec.Args, []string{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &item.Spec.Env, map[string]string{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(secretEnvJSON, &item.Spec.SecretEnv, map[string]string{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service secret env: %w", err)
	}
	if err := unmarshalJSON(filesJSON, &item.Spec.Files, []projectedfile.File{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service files: %w", err)
	}
	item.Spec.Files = projectedfile.CloneFiles(item.Spec.Files)
	if err := unmarshalJSON(runJSON, &item.Status.Run, controlservice.RunStatus{Phase: controlservice.RunPhasePending}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service run status: %w", err)
	}
	item.Status.Run.Phase = controlservice.NormalizeRunPhase(item.Status.Run.Phase)
	if lastReconciledAt.Valid {
		lastValue := lastReconciledAt.Time.UTC()
		item.Status.Observed.LastReconciledAt = &lastValue
	}
	if assignedPlaneID.Valid {
		item.Status.Observed.AssignedPlaneID = assignedPlaneID.String
	}
	return item, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func nullableOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func (s *Store) ensureServiceReferencesResolved(ctx context.Context, planeID string, registryCredentialID string) error {
	if _, err := s.GetPlane(ctx, planeID); err != nil {
		return err
	}
	if registryCredentialID != "" {
		if _, err := s.GetRegistryCredential(ctx, registryCredentialID); err != nil {
			return err
		}
	}
	return nil
}
