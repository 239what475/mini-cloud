-- +goose Up
CREATE TABLE IF NOT EXISTS control_events (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_control_events_created_at
    ON control_events (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_control_events_action_created_at
    ON control_events (action, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS fleet_planes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    grpc_endpoint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_plane_statuses (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TIMESTAMPTZ NULL,
    last_sync_at TIMESTAMPTZ NULL,
    last_inventory_version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plane_southbound_tokens (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    southbound_token TEXT NOT NULL,
    last_verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_services (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    spec_plane_id TEXT NOT NULL,
    spec_instance_class TEXT NOT NULL DEFAULT 'small',
    spec_exposure TEXT NOT NULL,
    spec_image TEXT NOT NULL,
    spec_command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_default_port INTEGER NOT NULL,
    spec_readiness_path TEXT NOT NULL,
    spec_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_secret_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_registry_server TEXT NOT NULL DEFAULT '',
    spec_registry_username TEXT NOT NULL DEFAULT '',
    spec_registry_password TEXT NOT NULL DEFAULT '',
    spec_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    status_run_json JSONB NOT NULL DEFAULT '{"phase":"pending","message":""}'::jsonb,
    generation BIGINT NOT NULL DEFAULT 1,
    status_desired_state TEXT NOT NULL DEFAULT 'active',
    status_observed_generation BIGINT NOT NULL DEFAULT 0,
    status_phase TEXT NOT NULL DEFAULT 'pending',
    status_healthy BOOLEAN NOT NULL DEFAULT false,
    status_message TEXT NOT NULL DEFAULT '',
    status_last_reconciled_at TIMESTAMPTZ NULL,
    status_assigned_plane_id TEXT NULL,
    status_remote_status TEXT NOT NULL DEFAULT '',
    status_remote_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fleet_services_name_key UNIQUE (name),
    CONSTRAINT fleet_services_plane_fk
        FOREIGN KEY (spec_plane_id) REFERENCES fleet_planes(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT fleet_services_assigned_plane_fk
        FOREIGN KEY (status_assigned_plane_id) REFERENCES fleet_planes(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_fleet_services_created_at
    ON fleet_services (created_at DESC);

CREATE TABLE IF NOT EXISTS fleet_plane_runtime_inventory_states (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    sync_version BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    nodes_total INTEGER NOT NULL,
    nodes_ready INTEGER NOT NULL,
    cpu_milli_capacity INTEGER NOT NULL,
    cpu_milli_allocated INTEGER NOT NULL,
    memory_mi_capacity INTEGER NOT NULL,
    memory_mi_allocated INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_plane_runtime_nodes (
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    node_epoch BIGINT NOT NULL DEFAULT 1,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    status TEXT NOT NULL,
    schedulable BOOLEAN NOT NULL,
    cpu_milli_capacity INTEGER NOT NULL,
    cpu_milli_allocated INTEGER NOT NULL,
    memory_mi_capacity INTEGER NOT NULL,
    memory_mi_allocated INTEGER NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plane_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_fleet_plane_runtime_nodes_plane_status
    ON fleet_plane_runtime_nodes (plane_id, status, schedulable);

CREATE TABLE IF NOT EXISTS fleet_plane_runtime_config_states (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    fingerprint TEXT NOT NULL,
    summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS fleet_plane_runtime_config_states;
DROP INDEX IF EXISTS idx_fleet_plane_runtime_nodes_plane_status;
DROP TABLE IF EXISTS fleet_plane_runtime_nodes;
DROP TABLE IF EXISTS fleet_plane_runtime_inventory_states;
DROP INDEX IF EXISTS idx_fleet_services_created_at;
DROP TABLE IF EXISTS fleet_services;
DROP TABLE IF EXISTS plane_southbound_tokens;
DROP TABLE IF EXISTS fleet_plane_statuses;
DROP TABLE IF EXISTS fleet_planes;
DROP INDEX IF EXISTS idx_control_events_action_created_at;
DROP INDEX IF EXISTS idx_control_events_created_at;
DROP TABLE IF EXISTS control_events;
