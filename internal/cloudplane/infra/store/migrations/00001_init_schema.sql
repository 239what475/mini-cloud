-- +goose Up
-- 当前仍处于开发期，cloud-plane 不保留旧 schema 的升级历史。
-- 这个 baseline 直接描述当前代码需要的完整数据库结构；已有开发库需要重建。

CREATE TABLE IF NOT EXISTS config_sets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT config_sets_name_unique UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS secret_sets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS registry_credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    server TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'runtime',
    private_ip TEXT NOT NULL,
    public_ip TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    cpu_milli_total INTEGER NOT NULL CHECK (cpu_milli_total > 0),
    memory_mi_total INTEGER NOT NULL CHECK (memory_mi_total > 0),
    cpu_milli_allocatable INTEGER NOT NULL DEFAULT 0 CHECK (cpu_milli_allocatable >= 0),
    memory_mi_allocatable INTEGER NOT NULL DEFAULT 0 CHECK (memory_mi_allocatable >= 0),
    cpu_milli_allocated INTEGER NOT NULL DEFAULT 0 CHECK (cpu_milli_allocated >= 0),
    memory_mi_allocated INTEGER NOT NULL DEFAULT 0 CHECK (memory_mi_allocated >= 0),
    status TEXT NOT NULL,
    schedulable BOOLEAN NOT NULL DEFAULT TRUE,
    last_heartbeat_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, instance_id)
);

CREATE TABLE IF NOT EXISTS node_heartbeats (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    reported_at TIMESTAMPTZ NOT NULL,
    agent_version TEXT NOT NULL,
    cpu_milli_allocatable INTEGER NOT NULL CHECK (cpu_milli_allocatable >= 0),
    memory_mi_allocatable INTEGER NOT NULL CHECK (memory_mi_allocatable >= 0),
    running_containers INTEGER NOT NULL CHECK (running_containers >= 0),
    status TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_node_heartbeats_node_reported_at
    ON node_heartbeats (node_id, reported_at DESC);

CREATE TABLE IF NOT EXISTS node_agent_session_tokens (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE CASCADE,
    token_prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_node_agent_session_tokens_last_used_at
    ON node_agent_session_tokens (last_used_at);

CREATE INDEX IF NOT EXISTS idx_node_agent_session_tokens_expires_at
    ON node_agent_session_tokens (expires_at);

CREATE TABLE IF NOT EXISTS services (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    spec_region TEXT NOT NULL,
    spec_replicas INTEGER NOT NULL CHECK (spec_replicas > 0),
    spec_instance_class TEXT NOT NULL,
    spec_exposure TEXT NOT NULL DEFAULT 'public',
    spec_image TEXT NOT NULL DEFAULT '',
    spec_command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_default_port INTEGER NOT NULL CHECK (spec_default_port > 0 AND spec_default_port <= 65535),
    spec_readiness_path TEXT NOT NULL,
    spec_config_set_id TEXT NULL REFERENCES config_sets(id) ON DELETE SET NULL,
    spec_secret_set_id TEXT NULL REFERENCES secret_sets(id) ON DELETE SET NULL,
    spec_registry_credential_id TEXT NULL REFERENCES registry_credentials(id) ON DELETE SET NULL,
    spec_projected_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_persistent_dirs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status_current_revision_id TEXT NULL,
    status_candidate_revision_id TEXT NULL,
    status_rollout_phase TEXT NOT NULL DEFAULT 'idle',
    status_rollout_message TEXT NOT NULL DEFAULT '',
    status_phase TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS idx_services_created_at
    ON services (created_at DESC);

CREATE TABLE IF NOT EXISTS revisions (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL,
    label TEXT NOT NULL,
    image TEXT NOT NULL,
    spec_command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_config_set_id TEXT NULL REFERENCES config_sets(id) ON DELETE SET NULL,
    spec_secret_set_id TEXT NULL REFERENCES secret_sets(id) ON DELETE SET NULL,
    spec_registry_credential_id TEXT NULL REFERENCES registry_credentials(id) ON DELETE SET NULL,
    spec_projected_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_persistent_dirs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    port INTEGER NOT NULL CHECK (port > 0 AND port <= 65535),
    readiness_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, label)
);

CREATE INDEX IF NOT EXISTS idx_revisions_service_created_at
    ON revisions (service_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_revisions_service_number
    ON revisions (service_id, revision_number);

ALTER TABLE services
    ADD CONSTRAINT services_current_revision_fk
    FOREIGN KEY (status_current_revision_id) REFERENCES revisions(id) ON DELETE SET NULL;

ALTER TABLE services
    ADD CONSTRAINT services_candidate_revision_fk
    FOREIGN KEY (status_candidate_revision_id) REFERENCES revisions(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
    desired_replicas INTEGER NOT NULL CHECK (desired_replicas > 0),
    ready_replicas INTEGER NOT NULL DEFAULT 0 CHECK (ready_replicas >= 0),
    available_replicas INTEGER NOT NULL DEFAULT 0 CHECK (available_replicas >= 0),
    status TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deployments_service_created_at
    ON deployments (service_id, created_at DESC);

CREATE TABLE IF NOT EXISTS deployment_transitions (
    id TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    from_status TEXT NULL,
    to_status TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deployment_transitions_created_at
    ON deployment_transitions (deployment_id, created_at ASC);

CREATE TABLE IF NOT EXISTS placement_decisions (
    id TEXT PRIMARY KEY,
    deployment_id TEXT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    replica_index INTEGER NOT NULL DEFAULT 0 CHECK (replica_index >= 0),
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    region TEXT NOT NULL,
    cpu_milli_request INTEGER NOT NULL CHECK (cpu_milli_request > 0),
    memory_mi_request INTEGER NOT NULL CHECK (memory_mi_request > 0),
    spec_replicas INTEGER NOT NULL CHECK (spec_replicas > 0),
    score BIGINT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_placement_decisions_created_at
    ON placement_decisions (created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_placement_decisions_deployment_replica
    ON placement_decisions (deployment_id, replica_index)
    WHERE deployment_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS deployment_executions (
    id TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    replica_index INTEGER NOT NULL DEFAULT 0 CHECK (replica_index >= 0),
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    image TEXT NOT NULL,
    container_name TEXT NOT NULL DEFAULT '',
    container_id TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL CHECK (container_port > 0 AND container_port <= 65535),
    host_port INTEGER NOT NULL DEFAULT 0 CHECK (host_port >= 0),
    readiness_path TEXT NOT NULL,
    status TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deployment_executions_deployment_created_at
    ON deployment_executions (deployment_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_deployment_executions_node_created_at
    ON deployment_executions (node_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_deployment_executions_deployment_replica
    ON deployment_executions (deployment_id, replica_index)
    WHERE status IN ('deploying', 'running');

CREATE TABLE IF NOT EXISTS execution_intents (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    service_id TEXT NOT NULL,
    service_name TEXT NOT NULL,
    service_exposure TEXT NOT NULL DEFAULT 'public',
    service_generation BIGINT NOT NULL CHECK (service_generation > 0),
    replica_index INTEGER NOT NULL CHECK (replica_index >= 0),
    node_id TEXT NULL REFERENCES nodes(id) ON DELETE SET NULL,
    image TEXT NOT NULL,
    command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    projected_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    persistent_dirs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    image_credential_server TEXT NULL,
    image_credential_username TEXT NULL,
    image_credential_password TEXT NULL,
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (plan_id, replica_index)
);

CREATE INDEX IF NOT EXISTS idx_execution_intents_status_created_at
    ON execution_intents (status, created_at ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_execution_intents_service_status
    ON execution_intents (service_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS runtime_nodes (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    instance_id TEXT NULL,
    instance_name TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    provisioned_at TIMESTAMPTZ NOT NULL,
    ready_at TIMESTAMPTZ NULL,
    last_synced_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, instance_id)
);

CREATE INDEX IF NOT EXISTS idx_runtime_nodes_created_at
    ON runtime_nodes (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_runtime_nodes_status_created_at
    ON runtime_nodes (status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS service_desired (
    service_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
    observed_generation BIGINT NOT NULL DEFAULT 0 CHECK (observed_generation >= 0),
    display_name TEXT NOT NULL,
    spec_region TEXT NOT NULL,
    spec_replicas INTEGER NOT NULL CHECK (spec_replicas > 0),
    spec_instance_class TEXT NOT NULL,
    spec_exposure TEXT NOT NULL,
    spec_image TEXT NOT NULL,
    spec_command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_default_port INTEGER NOT NULL CHECK (spec_default_port > 0 AND spec_default_port <= 65535),
    spec_readiness_path TEXT NOT NULL,
    spec_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_config_set_id TEXT NULL,
    spec_secret_set_id TEXT NULL,
    spec_registry_credential_id TEXT NULL,
    spec_projected_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_persistent_dirs_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_hash TEXT NOT NULL,
    reconcile_phase TEXT NOT NULL,
    reconcile_message TEXT NOT NULL DEFAULT '',
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    observed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS idx_service_desired_reconcile
    ON service_desired (reconcile_phase, updated_at ASC, service_id ASC);

CREATE INDEX IF NOT EXISTS idx_service_desired_observed_generation
    ON service_desired (service_id, generation, observed_generation);

CREATE TABLE IF NOT EXISTS deployment_rollout_metric_counters (
    result TEXT PRIMARY KEY,
    value BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deployment_rollout_outcome_marks (
    deployment_id TEXT PRIMARY KEY,
    result TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS runtime_node_bootstrap_metric_counters (
    result TEXT PRIMARY KEY,
    value BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS runtime_node_bootstrap_marks (
    provider TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    started_recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_recorded_at TIMESTAMPTZ NULL,
    PRIMARY KEY (provider, instance_id)
);

-- +goose Down
DROP TABLE IF EXISTS runtime_node_bootstrap_marks;
DROP TABLE IF EXISTS runtime_node_bootstrap_metric_counters;
DROP TABLE IF EXISTS deployment_rollout_outcome_marks;
DROP TABLE IF EXISTS deployment_rollout_metric_counters;
DROP TABLE IF EXISTS service_desired;
DROP TABLE IF EXISTS runtime_nodes;
DROP TABLE IF EXISTS execution_intents;
DROP TABLE IF EXISTS deployment_executions;
DROP TABLE IF EXISTS placement_decisions;
DROP TABLE IF EXISTS deployment_transitions;
DROP TABLE IF EXISTS deployments;
ALTER TABLE services DROP CONSTRAINT IF EXISTS services_candidate_revision_fk;
ALTER TABLE services DROP CONSTRAINT IF EXISTS services_current_revision_fk;
DROP TABLE IF EXISTS revisions;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS node_agent_session_tokens;
DROP TABLE IF EXISTS node_heartbeats;
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS registry_credentials;
DROP TABLE IF EXISTS secret_sets;
DROP TABLE IF EXISTS config_sets;
