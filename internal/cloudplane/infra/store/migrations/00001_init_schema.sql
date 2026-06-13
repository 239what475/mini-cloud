-- +goose Up
-- cloud-plane is still pre-release, so this baseline describes the current schema directly.
-- Existing development databases should be rebuilt instead of migrated through old shapes.

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    name TEXT NOT NULL,
    private_ip TEXT NOT NULL DEFAULT '',
    instance_id TEXT NULL,
    instance_type TEXT NOT NULL,
    cpu_milli_total INTEGER NOT NULL DEFAULT 0 CHECK (cpu_milli_total >= 0),
    memory_mi_total INTEGER NOT NULL DEFAULT 0 CHECK (memory_mi_total >= 0),
    cpu_milli_allocatable INTEGER NOT NULL DEFAULT 0 CHECK (cpu_milli_allocatable >= 0),
    memory_mi_allocatable INTEGER NOT NULL DEFAULT 0 CHECK (memory_mi_allocatable >= 0),
    cpu_milli_allocated INTEGER NOT NULL DEFAULT 0 CHECK (cpu_milli_allocated >= 0),
    memory_mi_allocated INTEGER NOT NULL DEFAULT 0 CHECK (memory_mi_allocated >= 0),
    status TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    schedulable BOOLEAN NOT NULL DEFAULT TRUE,
    elastic BOOLEAN NOT NULL DEFAULT FALSE,
    last_heartbeat_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, instance_id)
);

CREATE INDEX IF NOT EXISTS idx_nodes_status_created_at
    ON nodes (status, created_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS services (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    host TEXT NOT NULL UNIQUE,
    generation BIGINT NOT NULL CHECK (generation > 0),
    desired_state TEXT NOT NULL,
    instance_class TEXT NOT NULL,
    exposure TEXT NOT NULL,
    image TEXT NOT NULL,
    command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    container_port INTEGER NOT NULL CHECK (container_port > 0 AND container_port <= 65535),
    readiness_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_services_desired_updated_at
    ON services (desired_state, updated_at DESC);

CREATE TABLE IF NOT EXISTS service_runs (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL UNIQUE REFERENCES services(id) ON DELETE CASCADE,
    service_name TEXT NOT NULL,
    service_exposure TEXT NOT NULL DEFAULT 'public',
    service_generation BIGINT NOT NULL CHECK (service_generation > 0),
    node_id TEXT NULL REFERENCES nodes(id) ON DELETE SET NULL,
    image TEXT NOT NULL,
    command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    container_name TEXT NOT NULL DEFAULT '',
    container_id TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL CHECK (container_port > 0 AND container_port <= 65535),
    host_port INTEGER NOT NULL DEFAULT 0 CHECK (host_port >= 0),
    readiness_path TEXT NOT NULL,
    cpu_milli_request INTEGER NOT NULL CHECK (cpu_milli_request > 0),
    memory_mi_request INTEGER NOT NULL CHECK (memory_mi_request > 0),
    status TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_service_runs_status_created_at
    ON service_runs (status, created_at ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_service_runs_node_status
    ON service_runs (node_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS frontdoor_domains (
    host TEXT PRIMARY KEY,
    cname TEXT NOT NULL DEFAULT '',
    verify_subdomain TEXT NOT NULL DEFAULT '',
    verify_type TEXT NOT NULL DEFAULT '',
    verify_value TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS frontdoor_domains;
DROP TABLE IF EXISTS service_runs;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS nodes;
