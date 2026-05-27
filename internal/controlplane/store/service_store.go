package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	controlservice "mini-cloud/internal/controlplane/service"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceNotFound           = errors.New("service not found")
	ErrServiceNameAlreadyExists  = errors.New("service name already exists in this project")
	ErrServiceGenerationConflict = errors.New("service generation changed before reconcile write could be committed")
)

const serviceSelectColumns = `
	id,
	project_id,
	name,
	display_name,
	spec_provider,
	spec_region,
	spec_pinned_plane_id,
	spec_replicas,
	spec_instance_class,
	spec_exposure,
	spec_image,
	spec_command_json,
	spec_args_json,
	spec_default_port,
	spec_readiness_path,
	spec_env_json,
	spec_config_set_id,
	spec_secret_set_id,
	spec_registry_credential_id,
	spec_projected_files_json,
	spec_persistent_dirs_json,
	spec_persistent_dirs_locked,
	spec_revision_policy_json,
	status_rollout_json,
	generation,
	status_desired_state,
	status_observed_generation,
	status_phase,
	status_healthy,
	status_message,
	status_conditions_json,
	status_last_reconciled_at,
	created_at,
	updated_at
`

func (s *Store) CreateService(ctx context.Context, input controlservice.CreateInput) (controlservice.Service, error) {
	if err := input.Validate(); err != nil {
		return controlservice.Service{}, err
	}
	if err := s.ensureServiceResourceReferencesResolved(ctx, input.ProjectID, input.Spec.ConfigSetID, input.Spec.SecretSetID, input.Spec.RegistryCredentialID, input.Spec.ProjectedFiles); err != nil {
		return controlservice.Service{}, err
	}
	provider, region, pinnedPlaneID, replicas, instanceClass, revisionPolicy, err := controlservice.ResolveServicePlacementFields(input.Spec.Provider, input.Spec.Region, input.Spec.PinnedPlaneID, input.Spec.Replicas, input.Spec.InstanceClass, input.Spec.RevisionPolicy)
	if err != nil {
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
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(input.Spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service projected files: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(input.Spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service persistent dirs: %w", err)
	}
	revisionPolicyJSON, err := marshalJSON(revisionPolicy, controlservice.RevisionPolicy{}.Normalized())
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service revision policy: %w", err)
	}

	now := time.Now().UTC()
	initialStatus := controlservice.PendingStatus(0, now, controlservice.ReasonPendingCreate, "waiting for service reconcile")
	statusConditionsJSON, err := marshalJSON(initialStatus.Conditions, []controlservice.Condition{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service initial conditions: %w", err)
	}
	initialRollout := controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle}
	rolloutJSON, err := marshalJSON(initialRollout, controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service initial rollout: %w", err)
	}

	item, err := scanService(s.db.QueryRowContext(ctx, `
		INSERT INTO fleet_services (
			id,
			project_id,
			name,
			display_name,
			spec_provider,
			spec_region,
			spec_pinned_plane_id,
			spec_replicas,
			spec_instance_class,
			spec_exposure,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_default_port,
			spec_readiness_path,
			spec_env_json,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			spec_persistent_dirs_locked,
			spec_revision_policy_json,
			status_rollout_json,
			generation,
			status_desired_state,
			status_observed_generation,
			status_phase,
			status_healthy,
			status_message,
			status_conditions_json,
			status_last_reconciled_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, false, $22, $23, 1, $24, $25, $26, $27, $28, $29, NULL)
		RETURNING `+serviceSelectColumns+`
	`,
		id,
		input.ProjectID,
		input.Name,
		input.DisplayName,
		provider,
		region,
		nullableString(pinnedPlaneID),
		replicas,
		instanceClass,
		input.Spec.Exposure,
		input.Spec.Image,
		commandJSON,
		argsJSON,
		input.Spec.DefaultPort,
		input.Spec.ReadinessPath,
		envJSON,
		input.Spec.ConfigSetID,
		input.Spec.SecretSetID,
		input.Spec.RegistryCredentialID,
		projectedFilesJSON,
		persistentDirsJSON,
		revisionPolicyJSON,
		rolloutJSON,
		controlservice.DesiredStateActive,
		initialStatus.ObservedGeneration,
		initialStatus.Phase,
		initialStatus.Healthy,
		initialStatus.Message,
		statusConditionsJSON,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503":
				return controlservice.Service{}, ErrProjectNotFound
			case "23505":
				return controlservice.Service{}, ErrServiceNameAlreadyExists
			}
		}
		return controlservice.Service{}, fmt.Errorf("insert service: %w", err)
	}
	return item, nil
}

func (s *Store) ListServicesByProject(ctx context.Context, projectID string) ([]controlservice.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query services by project: %w", err)
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
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(input.Spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service projected files for update: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(input.Spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service persistent dirs for update: %w", err)
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
	if err := input.Validate(current.Metadata.ProjectID, current.Metadata.Name); err != nil {
		return controlservice.Service{}, err
	}
	if err := controlservice.ValidatePersistentDirUpdate(current, input); err != nil {
		return controlservice.Service{}, err
	}
	if err := s.ensureServiceResourceReferencesResolved(ctx, current.Metadata.ProjectID, input.Spec.ConfigSetID, input.Spec.SecretSetID, input.Spec.RegistryCredentialID, input.Spec.ProjectedFiles); err != nil {
		return controlservice.Service{}, err
	}
	provider, region, pinnedPlaneID, replicas, instanceClass, revisionPolicy, err := controlservice.ResolveServicePlacementFields(input.Spec.Provider, input.Spec.Region, input.Spec.PinnedPlaneID, input.Spec.Replicas, input.Spec.InstanceClass, input.Spec.RevisionPolicy)
	if err != nil {
		return controlservice.Service{}, err
	}
	revisionPolicyJSON, err := marshalJSON(revisionPolicy, controlservice.RevisionPolicy{}.Normalized())
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service revision policy for update: %w", err)
	}
	currentRolloutJSON, err := marshalJSON(current.Status.Rollout, controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service rollout for update: %w", err)
	}

	nextGeneration := current.Metadata.Generation + 1
	now := time.Now().UTC()
	pendingStatus := controlservice.PendingStatus(current.Status.Observed.ObservedGeneration, now, controlservice.ReasonSpecUpdated, "waiting for service reconcile")
	statusConditionsJSON, err := marshalJSON(pendingStatus.Conditions, []controlservice.Condition{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service update conditions: %w", err)
	}

	item, err := scanService(tx.QueryRowContext(ctx, `
		UPDATE fleet_services
		SET
			display_name = $2,
			spec_provider = $3,
			spec_region = $4,
			spec_pinned_plane_id = $5,
			spec_replicas = $6,
			spec_instance_class = $7,
			spec_exposure = $8,
			spec_image = $9,
			spec_command_json = $10,
			spec_args_json = $11,
			spec_default_port = $12,
			spec_readiness_path = $13,
			spec_env_json = $14,
			spec_config_set_id = $15,
			spec_secret_set_id = $16,
			spec_registry_credential_id = $17,
			spec_projected_files_json = $18,
			spec_persistent_dirs_json = $19,
			spec_revision_policy_json = $20,
			status_rollout_json = $21,
			generation = $22,
			status_desired_state = $23,
			status_observed_generation = $24,
			status_phase = $25,
			status_healthy = $26,
			status_message = $27,
			status_conditions_json = $28,
			status_last_reconciled_at = NULL,
			updated_at = now()
		WHERE id = $1
			AND generation = $29
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		input.DisplayName,
		provider,
		region,
		nullableString(pinnedPlaneID),
		replicas,
		instanceClass,
		input.Spec.Exposure,
		input.Spec.Image,
		commandJSON,
		argsJSON,
		input.Spec.DefaultPort,
		input.Spec.ReadinessPath,
		envJSON,
		input.Spec.ConfigSetID,
		input.Spec.SecretSetID,
		input.Spec.RegistryCredentialID,
		projectedFilesJSON,
		persistentDirsJSON,
		revisionPolicyJSON,
		currentRolloutJSON,
		nextGeneration,
		controlservice.DesiredStateActive,
		pendingStatus.ObservedGeneration,
		pendingStatus.Phase,
		pendingStatus.Healthy,
		pendingStatus.Message,
		statusConditionsJSON,
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
	now := time.Now().UTC()
	deletingStatus := controlservice.DeletingStatus(current.Status.Observed.ObservedGeneration, now, "waiting for remote service teardown")
	statusConditionsJSON, err := marshalJSON(deletingStatus.Conditions, []controlservice.Condition{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service delete conditions: %w", err)
	}
	currentRolloutJSON, err := marshalJSON(current.Status.Rollout, controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service rollout for delete: %w", err)
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
			status_conditions_json = $8,
			status_rollout_json = $9,
			status_last_reconciled_at = NULL,
			updated_at = now()
		WHERE id = $1
			AND generation = $10
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		nextGeneration,
		controlservice.DesiredStateDeleted,
		deletingStatus.ObservedGeneration,
		deletingStatus.Phase,
		deletingStatus.Healthy,
		deletingStatus.Message,
		statusConditionsJSON,
		currentRolloutJSON,
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

func (s *Store) LockServicePersistentDirs(ctx context.Context, serviceID string, generation int64) (controlservice.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		UPDATE fleet_services
		SET spec_persistent_dirs_locked = true, updated_at = now()
		WHERE id = $1 AND generation = $2
		RETURNING `+serviceSelectColumns+`
	`, serviceID, generation))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlservice.Service{}, ErrServiceGenerationConflict
		}
		return controlservice.Service{}, fmt.Errorf("lock service persistent dirs: %w", err)
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

	mergedConditions := mergeServiceConditions(current.Status.Observed.Conditions, input.Conditions)
	statusConditionsJSON, err := marshalJSON(mergedConditions, []controlservice.Condition{})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service status conditions: %w", err)
	}
	nextRollout := current.Status.Rollout
	if input.Rollout != nil {
		nextRollout = *input.Rollout
	}
	rolloutJSON, err := marshalJSON(nextRollout, controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle})
	if err != nil {
		return controlservice.Service{}, fmt.Errorf("marshal service rollout status: %w", err)
	}

	query := `
		UPDATE fleet_services
		SET
			status_observed_generation = $2,
			status_phase = $3,
			status_healthy = $4,
			status_message = $5,
			status_conditions_json = $6,
			status_last_reconciled_at = $7,
			status_rollout_json = $8,
			updated_at = now()
		WHERE id = $1
	`
	args := []any{
		serviceID,
		input.ObservedGeneration,
		input.Phase,
		input.Healthy,
		input.Message,
		statusConditionsJSON,
		input.LastReconciledAt,
		rolloutJSON,
	}
	if expectedGeneration != nil {
		query += ` AND generation = $9`
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

func mergeServiceConditions(previous []controlservice.Condition, next []controlservice.Condition) []controlservice.Condition {
	if len(next) == 0 {
		return nil
	}
	previousByType := make(map[string]controlservice.Condition, len(previous))
	for _, item := range previous {
		previousByType[item.Type] = item
	}

	merged := make([]controlservice.Condition, 0, len(next))
	for _, item := range next {
		if oldItem, ok := previousByType[item.Type]; ok &&
			oldItem.Status == item.Status &&
			oldItem.Reason == item.Reason &&
			oldItem.Message == item.Message &&
			oldItem.ObservedGeneration == item.ObservedGeneration {
			item.LastTransitionAt = oldItem.LastTransitionAt
		}
		merged = append(merged, item)
	}
	return merged
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
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var persistentDirsLocked bool
	var revisionPolicyJSON []byte
	var rolloutJSON []byte
	var conditionsJSON []byte
	var pinnedPlaneID sql.NullString
	var lastReconciledAt sql.NullTime
	if err := scanner.Scan(
		&item.Metadata.ID,
		&item.Metadata.ProjectID,
		&item.Metadata.Name,
		&item.Metadata.DisplayName,
		&item.Spec.Provider,
		&item.Spec.Region,
		&pinnedPlaneID,
		&item.Spec.Replicas,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&item.Spec.DefaultPort,
		&item.Spec.ReadinessPath,
		&envJSON,
		&item.Spec.ConfigSetID,
		&item.Spec.SecretSetID,
		&item.Spec.RegistryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&persistentDirsLocked,
		&revisionPolicyJSON,
		&rolloutJSON,
		&item.Metadata.Generation,
		&item.Status.DesiredState,
		&item.Status.Observed.ObservedGeneration,
		&item.Status.Observed.Phase,
		&item.Status.Observed.Healthy,
		&item.Status.Observed.Message,
		&conditionsJSON,
		&lastReconciledAt,
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
	if err := unmarshalJSON(projectedFilesJSON, &item.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service projected files: %w", err)
	}
	item.Spec.ProjectedFiles = projectedfile.CloneSpecs(item.Spec.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &item.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service persistent dirs: %w", err)
	}
	item.Spec.PersistentDirs = persistentdir.CloneSpecs(item.Spec.PersistentDirs)
	item.Spec.PersistentDirsLocked = persistentDirsLocked
	if err := unmarshalJSON(revisionPolicyJSON, &item.Spec.RevisionPolicy, controlservice.RevisionPolicy{}.Normalized()); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service revision policy: %w", err)
	}
	item.Spec.RevisionPolicy = item.Spec.RevisionPolicy.Normalized()
	if err := unmarshalJSON(rolloutJSON, &item.Status.Rollout, controlservice.RolloutStatus{Phase: controlservice.RolloutPhaseIdle}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service rollout status: %w", err)
	}
	item.Status.Rollout.Phase = controlservice.NormalizeRolloutPhase(item.Status.Rollout.Phase)
	if err := unmarshalJSON(conditionsJSON, &item.Status.Observed.Conditions, []controlservice.Condition{}); err != nil {
		return controlservice.Service{}, fmt.Errorf("decode service status conditions: %w", err)
	}
	if pinnedPlaneID.Valid {
		item.Spec.PinnedPlaneID = pinnedPlaneID.String
	}
	if lastReconciledAt.Valid {
		lastValue := lastReconciledAt.Time.UTC()
		item.Status.Observed.LastReconciledAt = &lastValue
	}
	return item, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func (s *Store) ensureServiceResourceReferencesResolved(ctx context.Context, projectID string, configSetID string, secretSetID string, registryCredentialID string, projectedFiles []projectedfile.Spec) error {
	configSets := make(map[string]map[string]string)
	secretSets := make(map[string]map[string]string)

	loadConfigSet := func(id string) (map[string]string, error) {
		if values, ok := configSets[id]; ok {
			return values, nil
		}
		item, err := s.GetProjectConfigSet(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		configSets[id] = item.Values
		return item.Values, nil
	}
	loadSecretSet := func(id string) (map[string]string, error) {
		if values, ok := secretSets[id]; ok {
			return values, nil
		}
		item, err := s.GetProjectSecretSet(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		secretSets[id] = item.Values
		return item.Values, nil
	}

	if configSetID != "" {
		if _, err := loadConfigSet(configSetID); err != nil {
			return err
		}
	}
	if secretSetID != "" {
		if _, err := loadSecretSet(secretSetID); err != nil {
			return err
		}
	}
	if registryCredentialID != "" {
		if _, err := s.GetProjectRegistryCredential(ctx, projectID, registryCredentialID); err != nil {
			return err
		}
	}
	for _, item := range projectedfile.CloneSpecs(projectedFiles) {
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			values, err := loadConfigSet(item.SourceID)
			if err != nil {
				return err
			}
			if _, ok := values[item.SourceKey]; !ok {
				return fmt.Errorf("config set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
		case projectedfile.SourceKindSecretSet:
			values, err := loadSecretSet(item.SourceID)
			if err != nil {
				return err
			}
			if _, ok := values[item.SourceKey]; !ok {
				return fmt.Errorf("secret set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
		default:
			return projectedfile.ErrSourceKindInvalid
		}
	}
	return nil
}
