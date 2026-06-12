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
	ErrServiceHostAlreadyExists  = errors.New("service host already exists")
	ErrServiceGenerationConflict = errors.New("service generation changed before status write could be committed")
	errServiceIDRequired         = errors.New("serviceID is required")
	errServiceNameRequired       = errors.New("name is required")
	errServiceHostRequired       = errors.New("host is required")
	errInvalidServiceName        = errors.New("name must use lowercase letters, digits, and hyphens")
	errDisplayNameRequired       = errors.New("displayName is required")
	errInvalidExposure           = errors.New("exposure must be one of public, private")
	errImageRequired             = errors.New("image is required")
	errInvalidDefaultPort        = errors.New("defaultPort must be between 1 and 65535")
	errInvalidReadinessPath      = errors.New("readinessPath must start with /")
	errInvalidEnvironmentKey     = errors.New("env keys must not be empty")
	errPlaneIDRequired           = errors.New("planeID is required")
	errServicePlaneImmutable     = errors.New("planeID cannot be changed after service creation")
	errInvalidInstanceClass      = errors.New("instanceClass must be one of small, medium, large")
	errInvalidServicePhase       = errors.New("service phase is invalid")
	errInvalidServiceRunPhase    = errors.New("service run phase is invalid")
	serviceNamePattern           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const serviceSelectColumns = `
	b.id,
	b.name,
	b.display_name,
	b.host,
	b.plane_id,
	c.instance_class,
	c.exposure,
	c.image,
	c.command_json,
	c.args_json,
	c.default_port,
	c.readiness_path,
	c.env_json,
	c.run_json,
	b.generation,
	b.desired_state,
	c.observed_generation,
	c.phase,
	c.message,
	c.last_observed_at,
	b.created_at,
	b.updated_at
`

type CreateServiceInput struct {
	Name        string
	DisplayName string
	Host        string
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
	LastObservedAt     *time.Time
	Run                *model.RunStatus
}

type UpsertServiceSnapshotInput struct {
	PlaneID       string
	Service       model.Service
	ObservedAt    time.Time
	StatusMessage string
}

type serviceSpecColumns struct {
	PlaneID       string
	InstanceClass string
	Exposure      string
	Image         string
	CommandJSON   []byte
	ArgsJSON      []byte
	DefaultPort   int
	ReadinessPath string
	EnvJSON       []byte
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
	if strings.TrimSpace(in.Host) == "" {
		return invalidInput(errServiceHostRequired)
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
	initialStatus := model.PendingServiceStatus(0, "waiting for cloud-plane service apply")
	runJSON, err := encodeServiceRun(model.PendingRunStatus("waiting for cloud-plane service apply"))
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service initial run: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Service{}, fmt.Errorf("begin create service binding tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO service_bindings (
			id,
			name,
			display_name,
			host,
			plane_id,
			generation,
			desired_state
		)
		VALUES ($1, $2, $3, $4, $5, 1, $6)
	`, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.DisplayName), cleanServiceDomain(input.Host), specColumns.PlaneID, model.DesiredStateActive); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "service_bindings_host_key" {
				return model.Service{}, ErrServiceHostAlreadyExists
			}
			return model.Service{}, ErrServiceNameAlreadyExists
		}
		return model.Service{}, fmt.Errorf("insert service binding: %w", err)
	}
	if err := upsertServiceCacheTx(ctx, tx, serviceCacheInput{
		ServiceID:          id,
		Spec:               specColumns,
		RunJSON:            runJSON,
		ObservedGeneration: initialStatus.ObservedGeneration,
		Phase:              initialStatus.Phase,
		Message:            initialStatus.Message,
		LastObservedAt:     nil,
	}); err != nil {
		return model.Service{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Service{}, fmt.Errorf("commit create service binding tx: %w", err)
	}
	return s.GetService(ctx, id)
}

func (s *Store) ListServices(ctx context.Context) ([]model.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings b
		JOIN service_caches c ON c.service_id = b.id
		ORDER BY b.created_at ASC, b.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query service bindings: %w", err)
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
		return nil, fmt.Errorf("iterate service bindings: %w", err)
	}
	return items, nil
}

func (s *Store) UpsertServiceSnapshot(ctx context.Context, input UpsertServiceSnapshotInput) error {
	service := input.Service
	if strings.TrimSpace(input.PlaneID) == "" {
		return invalidInput(errPlaneIDRequired)
	}
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return invalidInput(errServiceIDRequired)
	}
	if strings.TrimSpace(service.Metadata.Name) == "" {
		return invalidInput(errServiceNameRequired)
	}
	if strings.TrimSpace(service.Metadata.Host) == "" {
		return invalidInput(errServiceHostRequired)
	}
	specColumns, err := buildServiceSpecColumns(service.Spec)
	if err != nil {
		return err
	}
	specColumns.PlaneID = strings.TrimSpace(input.PlaneID)
	observedAt := input.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	message := strings.TrimSpace(input.StatusMessage)
	if message == "" {
		message = "observed from cloud-plane snapshot"
	}
	runJSON, err := encodeServiceRun(model.PendingRunStatus(message))
	if err != nil {
		return fmt.Errorf("marshal service cache run: %w", err)
	}
	desiredState := strings.TrimSpace(service.Status.DesiredState)
	if desiredState == "" {
		desiredState = model.DesiredStateActive
	}
	phase := model.PhaseProgressing
	if desiredState == model.DesiredStateDeleted {
		phase = model.PhaseDeleting
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert service cache tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO service_bindings (
			id,
			name,
			display_name,
			host,
			plane_id,
			generation,
			desired_state
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			host = EXCLUDED.host,
			plane_id = EXCLUDED.plane_id,
			generation = EXCLUDED.generation,
			desired_state = CASE
				WHEN service_bindings.desired_state = $8 THEN service_bindings.desired_state
				ELSE EXCLUDED.desired_state
			END,
			updated_at = now()
		WHERE service_bindings.generation <= EXCLUDED.generation
	`, service.Metadata.ID,
		strings.TrimSpace(service.Metadata.Name),
		strings.TrimSpace(service.Metadata.DisplayName),
		cleanServiceDomain(service.Metadata.Host),
		specColumns.PlaneID,
		service.Metadata.Generation,
		desiredState,
		model.DesiredStateDeleted,
	)
	if err != nil {
		return fmt.Errorf("upsert service binding snapshot: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return tx.Commit()
	}
	if err := upsertServiceCacheTx(ctx, tx, serviceCacheInput{
		ServiceID:          service.Metadata.ID,
		Spec:               specColumns,
		RunJSON:            runJSON,
		ObservedGeneration: service.Metadata.Generation,
		Phase:              phase,
		Message:            message,
		LastObservedAt:     &observedAt,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert service cache tx: %w", err)
	}
	return nil
}

func (s *Store) GetService(ctx context.Context, serviceID string) (model.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings b
		JOIN service_caches c ON c.service_id = b.id
		WHERE b.id = $1
	`, strings.TrimSpace(serviceID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service binding: %w", err)
	}
	return item, nil
}

func (s *Store) GetServiceByHost(ctx context.Context, host string) (model.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings b
		JOIN service_caches c ON c.service_id = b.id
		WHERE b.host = $1
	`, cleanServiceDomain(host)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service binding by host: %w", err)
	}
	return item, nil
}

func getServiceForUpdateTx(ctx context.Context, tx *sql.Tx, serviceID string) (model.Service, error) {
	item, err := scanService(tx.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings b
		JOIN service_caches c ON c.service_id = b.id
		WHERE b.id = $1
		FOR UPDATE OF b, c
	`, strings.TrimSpace(serviceID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service binding for update: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateService(ctx context.Context, serviceID string, input UpdateServiceInput) (model.Service, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Service{}, fmt.Errorf("begin update service binding tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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
	nextGeneration := current.Metadata.Generation + 1
	pendingStatus := model.PendingServiceStatus(current.Status.Observed.ObservedGeneration, "waiting for cloud-plane service apply")
	pendingRunJSON, err := encodeServiceRun(model.PendingRunStatus("waiting for cloud-plane service apply"))
	if err != nil {
		return model.Service{}, fmt.Errorf("marshal service run for update: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE service_bindings
		SET
			display_name = $2,
			generation = $3,
			desired_state = $4,
			updated_at = now()
		WHERE id = $1
		  AND generation = $5
	`, serviceID, strings.TrimSpace(input.DisplayName), nextGeneration, model.DesiredStateActive, current.Metadata.Generation)
	if err != nil {
		return model.Service{}, fmt.Errorf("update service binding: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
	}
	if err := upsertServiceCacheTx(ctx, tx, serviceCacheInput{
		ServiceID:          serviceID,
		Spec:               specColumns,
		RunJSON:            pendingRunJSON,
		ObservedGeneration: pendingStatus.ObservedGeneration,
		Phase:              pendingStatus.Phase,
		Message:            pendingStatus.Message,
		LastObservedAt:     nil,
	}); err != nil {
		return model.Service{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Service{}, fmt.Errorf("commit update service binding tx: %w", err)
	}
	return s.GetService(ctx, serviceID)
}

func (s *Store) MarkServiceDeletionRequested(ctx context.Context, serviceID string) (model.Service, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Service{}, fmt.Errorf("begin delete service binding tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	result, err := tx.ExecContext(ctx, `
		UPDATE service_bindings
		SET
			generation = $2,
			desired_state = $3,
			updated_at = now()
		WHERE id = $1
		  AND generation = $4
	`, serviceID, nextGeneration, model.DesiredStateDeleted, current.Metadata.Generation)
	if err != nil {
		return model.Service{}, fmt.Errorf("mark service binding deletion requested: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
	}
	if err := updateServiceCacheStatusTx(ctx, tx, serviceID, updateCacheStatusInput{
		ObservedGeneration: deletingStatus.ObservedGeneration,
		Phase:              deletingStatus.Phase,
		Message:            deletingStatus.Message,
		LastObservedAt:     nil,
		RunJSON:            pendingRunJSON,
	}); err != nil {
		return model.Service{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Service{}, fmt.Errorf("commit delete service binding tx: %w", err)
	}
	return s.GetService(ctx, serviceID)
}

func (s *Store) UpdateServiceStatusForGeneration(ctx context.Context, serviceID string, expectedGeneration int64, input UpdateServiceStatusInput) error {
	if !model.IsServicePhase(input.Phase) {
		return errInvalidServicePhase
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update service cache status tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getServiceForUpdateTx(ctx, tx, serviceID)
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
	if err := updateServiceCacheStatusTx(ctx, tx, serviceID, updateCacheStatusInput{
		ObservedGeneration: input.ObservedGeneration,
		Phase:              input.Phase,
		Message:            input.Message,
		LastObservedAt:     input.LastObservedAt,
		RunJSON:            runJSON,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update service cache status tx: %w", err)
	}
	return nil
}

func (s *Store) DeleteServiceForGeneration(ctx context.Context, serviceID string, expectedGeneration int64) error {
	row := s.db.QueryRowContext(ctx, `
		DELETE FROM service_bindings
		WHERE id = $1
		  AND generation = $2
		RETURNING id
	`, strings.TrimSpace(serviceID), expectedGeneration)
	var deletedID string
	if err := row.Scan(&deletedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return classifyServiceGenerationConflict(ctx, s, serviceID, expectedGeneration)
		}
		return fmt.Errorf("delete service binding for generation: %w", err)
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

type serviceCacheInput struct {
	ServiceID          string
	Spec               serviceSpecColumns
	RunJSON            []byte
	ObservedGeneration int64
	Phase              string
	Message            string
	LastObservedAt     *time.Time
}

func upsertServiceCacheTx(ctx context.Context, tx *sql.Tx, input serviceCacheInput) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO service_caches (
			service_id,
			instance_class,
			exposure,
			image,
			command_json,
			args_json,
			default_port,
			readiness_path,
			env_json,
			run_json,
			observed_generation,
			phase,
			message,
			last_observed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (service_id) DO UPDATE
		SET
			instance_class = EXCLUDED.instance_class,
			exposure = EXCLUDED.exposure,
			image = EXCLUDED.image,
			command_json = EXCLUDED.command_json,
			args_json = EXCLUDED.args_json,
			default_port = EXCLUDED.default_port,
			readiness_path = EXCLUDED.readiness_path,
			env_json = EXCLUDED.env_json,
			run_json = EXCLUDED.run_json,
			observed_generation = EXCLUDED.observed_generation,
			phase = EXCLUDED.phase,
			message = EXCLUDED.message,
			last_observed_at = EXCLUDED.last_observed_at,
			updated_at = now()
	`, strings.TrimSpace(input.ServiceID),
		input.Spec.InstanceClass,
		input.Spec.Exposure,
		input.Spec.Image,
		input.Spec.CommandJSON,
		input.Spec.ArgsJSON,
		input.Spec.DefaultPort,
		input.Spec.ReadinessPath,
		input.Spec.EnvJSON,
		input.RunJSON,
		input.ObservedGeneration,
		input.Phase,
		input.Message,
		input.LastObservedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert service cache: %w", err)
	}
	return nil
}

type updateCacheStatusInput struct {
	ObservedGeneration int64
	Phase              string
	Message            string
	LastObservedAt     *time.Time
	RunJSON            []byte
}

func updateServiceCacheStatusTx(ctx context.Context, tx *sql.Tx, serviceID string, input updateCacheStatusInput) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE service_caches
		SET
			observed_generation = $2,
			phase = $3,
			message = $4,
			last_observed_at = $5,
			run_json = $6,
			updated_at = now()
		WHERE service_id = $1
	`, strings.TrimSpace(serviceID), input.ObservedGeneration, input.Phase, input.Message, input.LastObservedAt, input.RunJSON)
	if err != nil {
		return fmt.Errorf("update service cache status: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func scanService(scanner interface{ Scan(dest ...any) error }) (model.Service, error) {
	var item model.Service
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var runJSON []byte
	var lastObservedAt sql.NullTime
	if err := scanner.Scan(
		&item.Metadata.ID,
		&item.Metadata.Name,
		&item.Metadata.DisplayName,
		&item.Metadata.Host,
		&item.Spec.PlaneID,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&item.Spec.DefaultPort,
		&item.Spec.ReadinessPath,
		&envJSON,
		&runJSON,
		&item.Metadata.Generation,
		&item.Status.DesiredState,
		&item.Status.Observed.ObservedGeneration,
		&item.Status.Observed.Phase,
		&item.Status.Observed.Message,
		&lastObservedAt,
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
	if len(runJSON) == 0 {
		return model.Service{}, fmt.Errorf("service run status is missing")
	}
	runStatus, err := decodeServiceRun(runJSON)
	if err != nil {
		return model.Service{}, fmt.Errorf("decode service run status: %w", err)
	}
	item.Status.Run = runStatus
	if lastObservedAt.Valid {
		lastValue := lastObservedAt.Time.UTC()
		item.Status.Observed.LastObservedAt = &lastValue
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
	return serviceSpecColumns{
		PlaneID:       planeID,
		InstanceClass: instanceClass,
		Exposure:      exposure,
		Image:         strings.TrimSpace(spec.Image),
		CommandJSON:   commandJSON,
		ArgsJSON:      argsJSON,
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: strings.TrimSpace(spec.ReadinessPath),
		EnvJSON:       envJSON,
	}, nil
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
