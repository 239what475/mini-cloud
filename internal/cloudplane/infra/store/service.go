package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

var (
	ErrServiceNotFound = errors.New("service not found")
)

const serviceSelectColumns = `
	id,
	name,
	display_name,
	host,
	generation,
	desired_state,
	instance_class,
	exposure,
	image,
	command_json,
	args_json,
	env_json,
	container_port,
	readiness_path,
	created_at,
	updated_at
`

func (s *Store) UpsertService(ctx context.Context, input cloudmodel.UpsertServiceInput) (cloudmodel.Service, error) {
	if err := input.Validate(); err != nil {
		return cloudmodel.Service{}, err
	}
	spec, err := serviceSpecRecord(input.Spec)
	if err != nil {
		return cloudmodel.Service{}, err
	}
	cpuMilli, memoryMi, err := cloudmodel.ResourceRequestForInstanceClass(input.Spec.InstanceClass)
	if err != nil {
		return cloudmodel.Service{}, err
	}
	exposure, err := cloudmodel.ParseExposure(input.Spec.Exposure)
	if err != nil {
		return cloudmodel.Service{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.Service{}, fmt.Errorf("begin upsert service tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	item, err := scanService(tx.QueryRowContext(ctx, `
		INSERT INTO services (
			id,
			name,
			display_name,
			host,
			generation,
			desired_state,
			instance_class,
			exposure,
			image,
			command_json,
			args_json,
			env_json,
			container_port,
			readiness_path
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE
		SET
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			host = EXCLUDED.host,
			generation = EXCLUDED.generation,
			desired_state = EXCLUDED.desired_state,
			instance_class = EXCLUDED.instance_class,
			exposure = EXCLUDED.exposure,
			image = EXCLUDED.image,
			command_json = EXCLUDED.command_json,
			args_json = EXCLUDED.args_json,
			env_json = EXCLUDED.env_json,
			container_port = EXCLUDED.container_port,
			readiness_path = EXCLUDED.readiness_path,
			updated_at = now()
		WHERE services.generation < EXCLUDED.generation
		   OR (
			services.generation = EXCLUDED.generation
			AND services.desired_state <> $15
		   )
		RETURNING `+serviceSelectColumns+`
	`,
		strings.TrimSpace(input.ID),
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.DisplayName),
		cleanDomain(input.Host),
		input.Generation,
		cloudmodel.ServiceDesiredActive,
		strings.TrimSpace(input.Spec.InstanceClass),
		exposure,
		strings.TrimSpace(input.Spec.Image),
		spec.CommandJSON,
		spec.ArgsJSON,
		spec.EnvJSON,
		input.Spec.ContainerPort,
		strings.TrimSpace(input.Spec.ReadinessPath),
		cloudmodel.ServiceDesiredDeleted,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			current, getErr := getServiceTx(ctx, tx, input.ID)
			if getErr != nil {
				return cloudmodel.Service{}, getErr
			}
			if err := tx.Commit(); err != nil {
				return cloudmodel.Service{}, fmt.Errorf("commit stale service upsert tx: %w", err)
			}
			return current, nil
		}
		return cloudmodel.Service{}, fmt.Errorf("upsert service: %w", err)
	}

	if err := upsertServiceRunTx(ctx, tx, serviceRunInput{
		ServiceID:         item.ID,
		ServiceName:       item.Name,
		ServiceGeneration: item.Generation,
		Image:             item.Spec.Image,
		Command:           item.Spec.Command,
		Args:              item.Spec.Args,
		Env:               item.Spec.Env,
		ContainerPort:     item.Spec.ContainerPort,
		ReadinessPath:     item.Spec.ReadinessPath,
		CPUMilliRequest:   cpuMilli,
		MemoryMiRequest:   memoryMi,
		Exposure:          exposure,
	}); err != nil {
		return cloudmodel.Service{}, err
	}

	if err := tx.Commit(); err != nil {
		return cloudmodel.Service{}, fmt.Errorf("commit upsert service tx: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteService(ctx context.Context, input cloudmodel.DeleteServiceInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete service tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getServiceTx(ctx, tx, input.ID)
	if err != nil {
		return err
	}
	generation := input.Generation
	if generation < current.Generation {
		generation = current.Generation
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE services
		SET
			generation = $2,
			desired_state = $3,
			updated_at = now()
		WHERE id = $1
	`, input.ID, generation, cloudmodel.ServiceDesiredDeleted); err != nil {
		return fmt.Errorf("mark service deleted: %w", err)
	}
	if err := deleteServiceRunTx(ctx, tx, input.ID, generation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete service tx: %w", err)
	}
	return nil
}

func (s *Store) ListServices(ctx context.Context) ([]cloudmodel.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM services
		WHERE desired_state = $1
		ORDER BY created_at ASC, id ASC
	`, cloudmodel.ServiceDesiredActive)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()

	items := make([]cloudmodel.Service, 0)
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

func getServiceTx(ctx context.Context, tx *sql.Tx, serviceID string) (cloudmodel.Service, error) {
	item, err := scanService(tx.QueryRowContext(ctx, `
		SELECT `+serviceSelectColumns+`
		FROM services
		WHERE id = $1
	`, strings.TrimSpace(serviceID)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudmodel.Service{}, ErrServiceNotFound
		}
		return cloudmodel.Service{}, fmt.Errorf("query service: %w", err)
	}
	return item, nil
}

type serviceSpecJSON struct {
	CommandJSON []byte
	ArgsJSON    []byte
	EnvJSON     []byte
}

func serviceSpecRecord(spec cloudmodel.ServiceSpec) (serviceSpecJSON, error) {
	command := spec.Command
	if command == nil {
		command = []string{}
	}
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return serviceSpecJSON{}, fmt.Errorf("marshal service command: %w", err)
	}
	args := spec.Args
	if args == nil {
		args = []string{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return serviceSpecJSON{}, fmt.Errorf("marshal service args: %w", err)
	}
	env := spec.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return serviceSpecJSON{}, fmt.Errorf("marshal service env: %w", err)
	}
	return serviceSpecJSON{CommandJSON: commandJSON, ArgsJSON: argsJSON, EnvJSON: envJSON}, nil
}

func scanService(scanner interface{ Scan(dest ...any) error }) (cloudmodel.Service, error) {
	var item cloudmodel.Service
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.DisplayName,
		&item.Host,
		&item.Generation,
		&item.DesiredState,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&envJSON,
		&item.Spec.ContainerPort,
		&item.Spec.ReadinessPath,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return cloudmodel.Service{}, err
	}
	item.Spec.Command = []string{}
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &item.Spec.Command); err != nil {
			return cloudmodel.Service{}, fmt.Errorf("decode service command: %w", err)
		}
	}
	item.Spec.Args = []string{}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &item.Spec.Args); err != nil {
			return cloudmodel.Service{}, fmt.Errorf("decode service args: %w", err)
		}
	}
	item.Spec.Env = map[string]string{}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &item.Spec.Env); err != nil {
			return cloudmodel.Service{}, fmt.Errorf("decode service env: %w", err)
		}
	}
	return item, nil
}
