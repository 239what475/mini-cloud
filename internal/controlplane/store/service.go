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
	"mini-cloud/internal/projectedfile"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceNotFound           = errors.New("service not found")
	ErrServiceNameAlreadyExists  = errors.New("service name already exists")
	ErrServiceGenerationConflict = errors.New("service generation changed before reconcile write could be committed")
	errServiceNameRequired       = errors.New("name is required")
	errInvalidServiceName        = errors.New("name must use lowercase letters, digits, and hyphens")
	errDisplayNameRequired       = errors.New("displayName is required")
	errInvalidExposure           = errors.New("exposure must be one of public, private")
	errImageRequired             = errors.New("image is required")
	errInvalidDefaultPort        = errors.New("defaultPort must be between 1 and 65535")
	errInvalidReadinessPath      = errors.New("readinessPath must start with /")
	errInvalidEnvironmentKey     = errors.New("env keys must not be empty")
	errRegistryServerRequired    = errors.New("registryCredential.server is required")
	errRegistryUsernameRequired  = errors.New("registryCredential.username is required")
	errRegistryPasswordRequired  = errors.New("registryCredential.password is required")
	errPlaneIDRequired           = errors.New("planeID is required")
	errInvalidInstanceClass      = errors.New("instanceClass must be one of small, medium, large")
	serviceNamePattern           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
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
	spec_registry_server,
	spec_registry_username,
	spec_registry_password,
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

type CreateServiceInput struct {
	Name        string
	DisplayName string
	Spec        model.ServiceSpec
}

type UpdateServiceInput struct {
	DisplayName string
	Spec        model.ServiceSpec
}

type UpdateServiceStatusInput struct {
	ObservedGeneration int64
	Phase              string
	Healthy            bool
	Message            string
	LastReconciledAt   *time.Time
	Run                *model.RunStatus
	AssignedPlaneID    *string
	RemoteStatus       *string
	RemoteMessage      *string
}

type serviceSpecColumns struct {
	PlaneID                  string
	InstanceClass            string
	Exposure                 string
	Image                    string
	CommandJSON              []byte
	ArgsJSON                 []byte
	DefaultPort              int
	ReadinessPath            string
	EnvJSON                  []byte
	SecretEnvJSON            []byte
	RegistryCredentialServer string
	RegistryCredentialUser   string
	RegistryCredentialPass   string
	FilesJSON                []byte
}

func (in CreateServiceInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return invalidInput(errInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	return validateServiceSpec(in.Spec)
}

func (in UpdateServiceInput) validate(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(serviceName)) {
		return invalidInput(errInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	return validateServiceSpec(in.Spec)
}

func (s *Store) CreateService(ctx context.Context, input CreateServiceInput) (model.Service, error) {
	if err := input.validate(); err != nil {
		return model.Service{}, err
	}
	specColumns, err := buildServiceSpecColumns(input.Spec)
	if err != nil {
		return model.Service{}, err
	}
	if err := s.ensureServiceReferencesResolved(ctx, specColumns.PlaneID); err != nil {
		return model.Service{}, err
	}

	id, err := newID("svc")
	if err != nil {
		return model.Service{}, err
	}

	initialStatus := model.PendingServiceStatus(0, "waiting for service reconcile")
	initialRun := model.RunStatus{Phase: model.RunPhasePending}
	runJSON, err := marshalJSON(initialRun, model.RunStatus{Phase: model.RunPhasePending})
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service initial run: %w", err)
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
			spec_registry_server,
			spec_registry_username,
			spec_registry_password,
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, 1, $19, $20, $21, $22, $23, NULL, NULL, '', '')
		RETURNING `+serviceSelectColumns+`
	`,
		id,
		input.Name,
		input.DisplayName,
		specColumns.PlaneID,
		specColumns.InstanceClass,
		specColumns.Exposure,
		specColumns.Image,
		specColumns.CommandJSON,
		specColumns.ArgsJSON,
		specColumns.DefaultPort,
		specColumns.ReadinessPath,
		specColumns.EnvJSON,
		specColumns.SecretEnvJSON,
		specColumns.RegistryCredentialServer,
		specColumns.RegistryCredentialUser,
		specColumns.RegistryCredentialPass,
		specColumns.FilesJSON,
		runJSON,
		model.DesiredStateActive,
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
				return model.Service{}, ErrServiceNameAlreadyExists
			}
		}
		return model.Service{}, fmt.Errorf("insert service: %w", err)
	}
	return item, nil
}

func (s *Store) ListServices(ctx context.Context) ([]model.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer closeRows(rows)

	items := make([]model.Service, 0)
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

func (s *Store) GetService(ctx context.Context, serviceID string) (model.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		WHERE id = $1
	`, serviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service: %w", err)
	}
	return item, nil
}

func getServiceForUpdateTx(ctx context.Context, tx *sql.Tx, serviceID string) (model.Service, error) {
	item, err := scanService(tx.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM fleet_services
		WHERE id = $1
		FOR UPDATE
	`, serviceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service for update: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateService(ctx context.Context, serviceID string, input UpdateServiceInput) (model.Service, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Service{}, fmt.Errorf("begin update service tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := getServiceForUpdateTx(ctx, tx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if err := input.validate(current.Metadata.Name); err != nil {
		return model.Service{}, err
	}
	specColumns, err := buildServiceSpecColumns(input.Spec)
	if err != nil {
		return model.Service{}, err
	}
	if err := s.ensureServiceReferencesResolved(ctx, specColumns.PlaneID); err != nil {
		return model.Service{}, err
	}
	currentRunJSON, err := marshalJSON(current.Status.Run, model.RunStatus{Phase: model.RunPhasePending})
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service run for update: %w", err)
	}

	nextGeneration := current.Metadata.Generation + 1
	pendingStatus := model.PendingServiceStatus(current.Status.Observed.ObservedGeneration, "waiting for service reconcile")

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
			spec_registry_server = $13,
			spec_registry_username = $14,
			spec_registry_password = $15,
			spec_files_json = $16,
			status_run_json = $17,
			generation = $18,
			status_desired_state = $19,
			status_observed_generation = $20,
			status_phase = $21,
			status_healthy = $22,
			status_message = $23,
			status_last_reconciled_at = NULL,
			status_remote_status = '',
			status_remote_message = '',
			updated_at = now()
		WHERE id = $1
			AND generation = $24
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		input.DisplayName,
		specColumns.PlaneID,
		specColumns.InstanceClass,
		specColumns.Exposure,
		specColumns.Image,
		specColumns.CommandJSON,
		specColumns.ArgsJSON,
		specColumns.DefaultPort,
		specColumns.ReadinessPath,
		specColumns.EnvJSON,
		specColumns.SecretEnvJSON,
		specColumns.RegistryCredentialServer,
		specColumns.RegistryCredentialUser,
		specColumns.RegistryCredentialPass,
		specColumns.FilesJSON,
		currentRunJSON,
		nextGeneration,
		model.DesiredStateActive,
		pendingStatus.ObservedGeneration,
		pendingStatus.Phase,
		pendingStatus.Healthy,
		pendingStatus.Message,
		current.Metadata.Generation,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return model.Service{}, fmt.Errorf("update service: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Service{}, fmt.Errorf("commit update service: %w", err)
	}
	return item, nil
}

func (s *Store) MarkServiceDeletionRequested(ctx context.Context, serviceID string) (model.Service, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Service{}, fmt.Errorf("begin delete service tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	current, err := getServiceForUpdateTx(ctx, tx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if current.Status.DesiredState == model.DesiredStateDeleted {
		return current, nil
	}

	nextGeneration := current.Metadata.Generation + 1
	deletingStatus := model.DeletingServiceStatus(current.Status.Observed.ObservedGeneration, "waiting for remote service teardown")
	currentRunJSON, err := marshalJSON(current.Status.Run, model.RunStatus{Phase: model.RunPhasePending})
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service run for delete: %w", err)
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
		model.DesiredStateDeleted,
		deletingStatus.ObservedGeneration,
		deletingStatus.Phase,
		deletingStatus.Healthy,
		deletingStatus.Message,
		currentRunJSON,
		current.Metadata.Generation,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return model.Service{}, fmt.Errorf("mark service deletion requested: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Service{}, fmt.Errorf("commit delete service: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateServiceStatusForGeneration(ctx context.Context, serviceID string, expectedGeneration int64, input UpdateServiceStatusInput) error {
	current, err := s.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	if current.Metadata.Generation != expectedGeneration {
		return ErrServiceGenerationConflict
	}

	nextRun := current.Status.Run
	if input.Run != nil {
		nextRun = model.CloneRunStatus(*input.Run)
	}
	runJSON, err := marshalJSON(nextRun, model.RunStatus{Phase: model.RunPhasePending})
	if err != nil {
		return fmt.Errorf("marshal service run status: %w", err)
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
		AND generation = $11
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
		expectedGeneration,
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update service status: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return classifyServiceGenerationConflict(ctx, s, serviceID, expectedGeneration)
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

func scanService(scanner interface{ Scan(dest ...any) error }) (model.Service, error) {
	var item model.Service
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var secretEnvJSON []byte
	var registryServerValue string
	var registryUsernameValue string
	var registryPasswordValue string
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
		&registryServerValue,
		&registryUsernameValue,
		&registryPasswordValue,
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
		return model.Service{}, err
	}
	if err := unmarshalJSON(commandJSON, &item.Spec.Command, []string{}); err != nil {
		return model.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &item.Spec.Args, []string{}); err != nil {
		return model.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &item.Spec.Env, map[string]string{}); err != nil {
		return model.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(secretEnvJSON, &item.Spec.SecretEnv, map[string]string{}); err != nil {
		return model.Service{}, fmt.Errorf("decode service secret env: %w", err)
	}
	if err := unmarshalJSON(filesJSON, &item.Spec.Files, []projectedfile.File{}); err != nil {
		return model.Service{}, fmt.Errorf("decode service files: %w", err)
	}
	item.Spec.Files = projectedfile.CloneFiles(item.Spec.Files)
	item.Spec.RegistryCredential = registryCredentialFromColumns(registryServerValue, registryUsernameValue, registryPasswordValue)
	if err := unmarshalJSON(runJSON, &item.Status.Run, model.RunStatus{Phase: model.RunPhasePending}); err != nil {
		return model.Service{}, fmt.Errorf("decode service run status: %w", err)
	}
	item.Status.Run.Phase = normalizeRunPhase(item.Status.Run.Phase)
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

func normalizeRunPhase(phase string) string {
	value := strings.ToLower(strings.TrimSpace(phase))
	if value == "" {
		return model.RunPhasePending
	}
	return value
}

func nullableOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func (s *Store) ensureServiceReferencesResolved(ctx context.Context, planeID string) error {
	if _, err := s.GetPlane(ctx, planeID); err != nil {
		return err
	}
	return nil
}

func registryCredentialFromColumns(server string, username string, password string) *model.ServiceRegistryCredential {
	if strings.TrimSpace(server) == "" && strings.TrimSpace(username) == "" && password == "" {
		return nil
	}
	return &model.ServiceRegistryCredential{
		Server:   server,
		Username: username,
		Password: password,
	}
}

func validateServiceSpec(spec model.ServiceSpec) error {
	if _, _, err := resolveServicePlacementFields(spec.PlaneID, spec.InstanceClass); err != nil {
		return invalidInput(err)
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return invalidInput(errInvalidExposure)
	}
	if strings.TrimSpace(spec.Image) == "" {
		return invalidInput(errImageRequired)
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return invalidInput(errInvalidDefaultPort)
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return invalidInput(errInvalidReadinessPath)
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return invalidInput(errInvalidEnvironmentKey)
		}
	}
	for key := range spec.SecretEnv {
		if strings.TrimSpace(key) == "" {
			return invalidInput(errInvalidEnvironmentKey)
		}
	}
	if err := projectedfile.ValidateFiles(spec.Files); err != nil {
		return invalidInput(err)
	}
	if err := validateRegistryCredential(spec.RegistryCredential); err != nil {
		return invalidInput(err)
	}
	return nil
}

func validateRegistryCredential(input *model.ServiceRegistryCredential) error {
	if input == nil {
		return nil
	}
	if strings.TrimSpace(input.Server) == "" {
		return errRegistryServerRequired
	}
	if strings.TrimSpace(input.Username) == "" {
		return errRegistryUsernameRequired
	}
	if strings.TrimSpace(input.Password) == "" {
		return errRegistryPasswordRequired
	}
	return nil
}

func buildServiceSpecColumns(spec model.ServiceSpec) (serviceSpecColumns, error) {
	planeID, instanceClass, err := resolveServicePlacementFields(spec.PlaneID, spec.InstanceClass)
	if err != nil {
		return serviceSpecColumns{}, err
	}
	commandJSON, err := marshalJSON(spec.Command, []string{})
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service command: %w", err)
	}
	argsJSON, err := marshalJSON(spec.Args, []string{})
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service args: %w", err)
	}
	envJSON, err := marshalJSON(spec.Env, map[string]string{})
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service env: %w", err)
	}
	secretEnvJSON, err := marshalJSON(spec.SecretEnv, map[string]string{})
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service secret env: %w", err)
	}
	filesJSON, err := marshalJSON(projectedfile.CloneFiles(spec.Files), []projectedfile.File{})
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service files: %w", err)
	}

	out := serviceSpecColumns{
		PlaneID:       planeID,
		InstanceClass: instanceClass,
		Exposure:      spec.Exposure,
		Image:         spec.Image,
		CommandJSON:   commandJSON,
		ArgsJSON:      argsJSON,
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: spec.ReadinessPath,
		EnvJSON:       envJSON,
		SecretEnvJSON: secretEnvJSON,
		FilesJSON:     filesJSON,
	}
	if spec.RegistryCredential != nil {
		out.RegistryCredentialServer = spec.RegistryCredential.Server
		out.RegistryCredentialUser = spec.RegistryCredential.Username
		out.RegistryCredentialPass = spec.RegistryCredential.Password
	}
	return out, nil
}

func resolveServicePlacementFields(planeID string, instanceClass string) (string, string, error) {
	if strings.TrimSpace(planeID) == "" {
		return "", "", errPlaneIDRequired
	}
	if !model.IsInstanceClass(instanceClass) {
		return "", "", errInvalidInstanceClass
	}
	return strings.TrimSpace(planeID), instanceClass, nil
}
