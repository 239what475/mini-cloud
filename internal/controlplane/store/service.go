package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"

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
	errServicePlaneImmutable     = errors.New("planeID cannot be changed after service creation")
	errInvalidInstanceClass      = errors.New("instanceClass must be one of small, medium, large")
	errInvalidServicePhase       = errors.New("service phase is invalid")
	errInvalidServiceRunPhase    = errors.New("service run phase is invalid")
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
	status_run_json,
	generation,
	status_desired_state,
	status_observed_generation,
	status_phase,
	status_message,
	status_last_reconciled_at,
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
	Message            string
	LastReconciledAt   *time.Time
	Run                *model.RunStatus
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
}

type serviceRunRecord struct {
	CurrentRunID   string     `json:"currentRunID,omitempty"`
	LatestRunID    string     `json:"latestRunID,omitempty"`
	Phase          string     `json:"phase"`
	Message        string     `json:"message,omitempty"`
	LastObservedAt *time.Time `json:"lastObservedAt,omitempty"`
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

func (in UpdateServiceInput) validate(current model.Service) error {
	if strings.TrimSpace(current.Metadata.Name) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(current.Metadata.Name)) {
		return invalidInput(errInvalidServiceName)
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	if strings.TrimSpace(in.Spec.PlaneID) != strings.TrimSpace(current.Spec.PlaneID) {
		return invalidInput(errServicePlaneImmutable)
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
	if _, err := s.GetPlane(ctx, specColumns.PlaneID); err != nil {
		return model.Service{}, err
	}

	id, err := newID("svc")
	if err != nil {
		return model.Service{}, err
	}

	initialStatus := model.PendingServiceStatus(0, "waiting for service reconcile")
	runJSON, err := encodeServiceRun(model.PendingRunStatus("waiting for service reconcile"))
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service initial run: %w", err)
	}

	item, err := scanService(s.db.QueryRowContext(ctx, `
		INSERT INTO services (
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
			status_run_json,
			generation,
			status_desired_state,
			status_observed_generation,
			status_phase,
			status_message,
			status_last_reconciled_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 1, $18, $19, $20, $21, NULL)
		RETURNING `+serviceSelectColumns+`
	`,
		id,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.DisplayName),
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
		runJSON,
		model.DesiredStateActive,
		initialStatus.ObservedGeneration,
		initialStatus.Phase,
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
		FROM services
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()

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
		FROM services
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
		FROM services
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
	if err := input.validate(current); err != nil {
		return model.Service{}, err
	}
	specColumns, err := buildServiceSpecColumns(input.Spec)
	if err != nil {
		return model.Service{}, err
	}
	if _, err := s.GetPlane(ctx, specColumns.PlaneID); err != nil {
		return model.Service{}, err
	}
	pendingRunJSON, err := encodeServiceRun(model.PendingRunStatus("waiting for service reconcile"))
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service run for update: %w", err)
	}

	nextGeneration := current.Metadata.Generation + 1
	pendingStatus := model.PendingServiceStatus(current.Status.Observed.ObservedGeneration, "waiting for service reconcile")

	item, err := scanService(tx.QueryRowContext(ctx, `
		UPDATE services
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
			status_run_json = $16,
			generation = $17,
			status_desired_state = $18,
			status_observed_generation = $19,
			status_phase = $20,
			status_message = $21,
			status_last_reconciled_at = NULL,
			updated_at = now()
		WHERE id = $1
			AND generation = $22
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		strings.TrimSpace(input.DisplayName),
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
		pendingRunJSON,
		nextGeneration,
		model.DesiredStateActive,
		pendingStatus.ObservedGeneration,
		pendingStatus.Phase,
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
	pendingRunJSON, err := encodeServiceRun(model.PendingRunStatus("waiting for remote service teardown"))
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service run for delete: %w", err)
	}

	item, err := scanService(tx.QueryRowContext(ctx, `
		UPDATE services
		SET
			generation = $2,
			status_desired_state = $3,
			status_observed_generation = $4,
			status_phase = $5,
			status_message = $6,
			status_run_json = $7,
			status_last_reconciled_at = NULL,
			updated_at = now()
		WHERE id = $1
			AND generation = $8
		RETURNING `+serviceSelectColumns+`
	`,
		serviceID,
		nextGeneration,
		model.DesiredStateDeleted,
		deletingStatus.ObservedGeneration,
		deletingStatus.Phase,
		deletingStatus.Message,
		pendingRunJSON,
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
	if !model.IsServicePhase(input.Phase) {
		return errInvalidServicePhase
	}
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
	if !model.IsRunPhase(nextRun.Phase) {
		return errInvalidServiceRunPhase
	}
	runJSON, err := encodeServiceRun(nextRun)
	if err != nil {
		return fmt.Errorf("marshal service run status: %w", err)
	}
	query := `
		UPDATE services
		SET
			status_observed_generation = $2,
			status_phase = $3,
			status_message = $4,
			status_last_reconciled_at = $5,
			status_run_json = $6,
			updated_at = now()
		WHERE id = $1
			AND generation = $7
	`
	args := []any{
		serviceID,
		input.ObservedGeneration,
		input.Phase,
		input.Message,
		input.LastReconciledAt,
		runJSON,
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
		DELETE FROM services
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
	var runJSON []byte
	var lastReconciledAt sql.NullTime
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
		&runJSON,
		&item.Metadata.Generation,
		&item.Status.DesiredState,
		&item.Status.Observed.ObservedGeneration,
		&item.Status.Observed.Phase,
		&item.Status.Observed.Message,
		&lastReconciledAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.Service{}, err
	}
	item.Spec.Command = []string{}
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &item.Spec.Command); err != nil {
			return model.Service{}, fmt.Errorf("decode service command: %w", err)
		}
	}
	if item.Spec.Command == nil {
		item.Spec.Command = []string{}
	}
	item.Spec.Args = []string{}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &item.Spec.Args); err != nil {
			return model.Service{}, fmt.Errorf("decode service args: %w", err)
		}
	}
	if item.Spec.Args == nil {
		item.Spec.Args = []string{}
	}
	item.Spec.Env = map[string]string{}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &item.Spec.Env); err != nil {
			return model.Service{}, fmt.Errorf("decode service env: %w", err)
		}
	}
	if item.Spec.Env == nil {
		item.Spec.Env = map[string]string{}
	}
	item.Spec.SecretEnv = map[string]string{}
	if len(secretEnvJSON) > 0 {
		if err := json.Unmarshal(secretEnvJSON, &item.Spec.SecretEnv); err != nil {
			return model.Service{}, fmt.Errorf("decode service secret env: %w", err)
		}
	}
	if item.Spec.SecretEnv == nil {
		item.Spec.SecretEnv = map[string]string{}
	}
	if strings.TrimSpace(registryServerValue) != "" || strings.TrimSpace(registryUsernameValue) != "" || registryPasswordValue != "" {
		item.Spec.RegistryCredential = &model.ServiceRegistryCredential{
			Server:   registryServerValue,
			Username: registryUsernameValue,
			Password: registryPasswordValue,
		}
	}
	if len(runJSON) == 0 {
		return model.Service{}, fmt.Errorf("service run status is missing")
	}
	runStatus, err := decodeServiceRun(runJSON)
	if err != nil {
		return model.Service{}, fmt.Errorf("decode service run status: %w", err)
	}
	item.Status.Run = runStatus
	if lastReconciledAt.Valid {
		lastValue := lastReconciledAt.Time.UTC()
		item.Status.Observed.LastReconciledAt = &lastValue
	}
	return item, nil
}

func encodeServiceRun(input model.RunStatus) ([]byte, error) {
	record := serviceRunRecord{
		CurrentRunID:   strings.TrimSpace(input.CurrentRunID),
		LatestRunID:    strings.TrimSpace(input.LatestRunID),
		Phase:          strings.TrimSpace(input.Phase),
		Message:        input.Message,
		LastObservedAt: input.LastObservedAt,
	}
	if !model.IsRunPhase(record.Phase) {
		return nil, errInvalidServiceRunPhase
	}
	if record.LastObservedAt != nil {
		value := record.LastObservedAt.UTC()
		record.LastObservedAt = &value
	}
	return json.Marshal(record)
}

func decodeServiceRun(data []byte) (model.RunStatus, error) {
	if len(data) == 0 {
		return model.RunStatus{}, fmt.Errorf("service run status is missing")
	}
	var record serviceRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return model.RunStatus{}, err
	}
	if !model.IsRunPhase(record.Phase) {
		return model.RunStatus{}, errInvalidServiceRunPhase
	}
	out := model.RunStatus{
		CurrentRunID:   record.CurrentRunID,
		LatestRunID:    record.LatestRunID,
		Phase:          record.Phase,
		Message:        record.Message,
		LastObservedAt: record.LastObservedAt,
	}
	if out.LastObservedAt != nil {
		value := out.LastObservedAt.UTC()
		out.LastObservedAt = &value
	}
	return out, nil
}

func validateServiceSpec(spec model.ServiceSpec) error {
	if _, _, err := resolveServicePlacementFields(spec.PlaneID, spec.InstanceClass); err != nil {
		return invalidInput(err)
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = model.ExposurePublic
	}
	if !model.IsServiceExposure(resolvedExposure) {
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
	exposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if exposure == "" {
		exposure = model.ExposurePublic
	}
	command := spec.Command
	if command == nil {
		command = []string{}
	}
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service command: %w", err)
	}
	args := spec.Args
	if args == nil {
		args = []string{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service args: %w", err)
	}
	env := spec.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service env: %w", err)
	}
	secretEnv := spec.SecretEnv
	if secretEnv == nil {
		secretEnv = map[string]string{}
	}
	secretEnvJSON, err := json.Marshal(secretEnv)
	if err != nil {
		return serviceSpecColumns{}, fmt.Errorf("marshal service secret env: %w", err)
	}
	out := serviceSpecColumns{
		PlaneID:       planeID,
		InstanceClass: instanceClass,
		Exposure:      exposure,
		Image:         strings.TrimSpace(spec.Image),
		CommandJSON:   commandJSON,
		ArgsJSON:      argsJSON,
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: strings.TrimSpace(spec.ReadinessPath),
		EnvJSON:       envJSON,
		SecretEnvJSON: secretEnvJSON,
	}
	if spec.RegistryCredential != nil {
		out.RegistryCredentialServer = strings.TrimSpace(spec.RegistryCredential.Server)
		out.RegistryCredentialUser = strings.TrimSpace(spec.RegistryCredential.Username)
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
