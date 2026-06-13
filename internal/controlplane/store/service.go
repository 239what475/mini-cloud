package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"mini-cloud/internal/controlplane/model"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceNotFound           = errors.New("service not found")
	ErrServiceNameAlreadyExists  = errors.New("service name already exists")
	ErrServiceHostAlreadyExists  = errors.New("service host already exists")
	ErrServiceDeleting           = errors.New("service is deleting")
	ErrServiceGenerationConflict = errors.New("service generation changed before status write could be committed")
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
	errInvalidInstanceClass      = errors.New("instanceClass must be one of small, medium, large")
	serviceNamePattern           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const serviceSelectColumns = `
	id,
	name,
	display_name,
	host,
	plane_id,
	generation,
	desired_state,
	created_at,
	updated_at
`

type CreateServiceInput struct {
	Name        string
	DisplayName string
	Host        string
	Spec        model.ServiceSpec
}

type UpdateServiceInput struct {
	DisplayName string
	Spec        model.WorkloadSpec
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

func (in UpdateServiceInput) validate() error {
	if strings.TrimSpace(in.DisplayName) == "" {
		return invalidInput(errDisplayNameRequired)
	}
	return validateWorkloadSpec(in.Spec)
}

func (s *Store) CreateService(ctx context.Context, input CreateServiceInput) (model.Service, error) {
	if err := input.validate(); err != nil {
		return model.Service{}, err
	}
	planeID := strings.TrimSpace(input.Spec.PlaneID)
	if _, err := s.GetPlane(ctx, planeID); err != nil {
		return model.Service{}, err
	}

	id, err := newID("svc")
	if err != nil {
		return model.Service{}, err
	}
	item, err := scanService(s.db.QueryRowContext(ctx, `
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
		RETURNING `+serviceSelectColumns+`
	`, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.DisplayName), cleanServiceDomain(input.Host), planeID, model.DesiredStateActive))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "service_bindings_host_key" {
				return model.Service{}, ErrServiceHostAlreadyExists
			}
			return model.Service{}, ErrServiceNameAlreadyExists
		}
		return model.Service{}, fmt.Errorf("insert service binding: %w", err)
	}
	return item, nil
}

func (s *Store) ListServices(ctx context.Context) ([]model.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings
		ORDER BY created_at ASC, id ASC
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

func (s *Store) ListDeletingServices(ctx context.Context) ([]model.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings
		WHERE desired_state = $1
		ORDER BY created_at ASC, id ASC
	`, model.DesiredStateDeleted)
	if err != nil {
		return nil, fmt.Errorf("query deleting service bindings: %w", err)
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
		return nil, fmt.Errorf("iterate deleting service bindings: %w", err)
	}
	return items, nil
}

func (s *Store) GetService(ctx context.Context, serviceID string) (model.Service, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM service_bindings
		WHERE id = $1
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
		FROM service_bindings
		WHERE host = $1
	`, cleanServiceDomain(host)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, ErrServiceNotFound
		}
		return model.Service{}, fmt.Errorf("query service binding by host: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateServiceBinding(ctx context.Context, serviceID string, input UpdateServiceInput) (model.Service, error) {
	if err := input.validate(); err != nil {
		return model.Service{}, err
	}
	current, err := s.GetService(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if current.Status.DesiredState == model.DesiredStateDeleted {
		return model.Service{}, ErrServiceDeleting
	}

	nextGeneration := current.Metadata.Generation + 1
	updated, err := scanService(s.db.QueryRowContext(ctx, `
		UPDATE service_bindings
		SET
			display_name = $2,
			generation = $3,
			desired_state = $4,
			updated_at = now()
		WHERE id = $1
		  AND generation = $5
		RETURNING `+serviceSelectColumns+`
	`, serviceID, strings.TrimSpace(input.DisplayName), nextGeneration, model.DesiredStateActive, current.Metadata.Generation))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return model.Service{}, fmt.Errorf("update service binding: %w", err)
	}
	return updated, nil
}

func (s *Store) MarkServiceDeletionRequested(ctx context.Context, serviceID string) (model.Service, error) {
	current, err := s.GetService(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if current.Status.DesiredState == model.DesiredStateDeleted {
		return current, nil
	}

	nextGeneration := current.Metadata.Generation + 1
	deleting, err := scanService(s.db.QueryRowContext(ctx, `
		UPDATE service_bindings
		SET
			generation = $2,
			desired_state = $3,
			updated_at = now()
		WHERE id = $1
		  AND generation = $4
		RETURNING `+serviceSelectColumns+`
	`, serviceID, nextGeneration, model.DesiredStateDeleted, current.Metadata.Generation))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Service{}, classifyServiceGenerationConflict(ctx, s, serviceID, current.Metadata.Generation)
		}
		return model.Service{}, fmt.Errorf("mark service binding deletion requested: %w", err)
	}
	return deleting, nil
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

func scanService(scanner interface{ Scan(dest ...any) error }) (model.Service, error) {
	var item model.Service
	if err := scanner.Scan(
		&item.Metadata.ID,
		&item.Metadata.Name,
		&item.Metadata.DisplayName,
		&item.Metadata.Host,
		&item.Spec.PlaneID,
		&item.Metadata.Generation,
		&item.Status.DesiredState,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.Service{}, err
	}
	item.Status.Observed = model.PendingServiceStatus(0, "waiting for cloud-plane snapshot")
	item.Status.Run = model.PendingRunStatus("waiting for cloud-plane snapshot")
	return item, nil
}

func validateServiceSpec(spec model.ServiceSpec) error {
	if strings.TrimSpace(spec.PlaneID) == "" {
		return invalidInput(errPlaneIDRequired)
	}
	if err := validateWorkloadSpec(model.WorkloadSpec{
		InstanceClass: spec.InstanceClass,
		Exposure:      spec.Exposure,
		Image:         spec.Image,
		Command:       spec.Command,
		Args:          spec.Args,
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: spec.ReadinessPath,
		Env:           spec.Env,
	}); err != nil {
		return err
	}
	return nil
}

func validateWorkloadSpec(spec model.WorkloadSpec) error {
	if !model.IsInstanceClass(spec.InstanceClass) {
		return invalidInput(errInvalidInstanceClass)
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
