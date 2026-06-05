-- +goose Up
-- 当前仍处于开发期，cloud-plane 不保留旧 schema 的升级历史。
-- 这个 baseline 直接描述当前代码需要的完整数据库结构；已有开发库需要重建。

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

CREATE TABLE IF NOT EXISTS execution_intents (
    id TEXT PRIMARY KEY,
    work_action TEXT NOT NULL DEFAULT 'run',
    plan_id TEXT NOT NULL,
    service_id TEXT NOT NULL,
    service_name TEXT NOT NULL,
    service_exposure TEXT NOT NULL DEFAULT 'public',
    service_generation BIGINT NOT NULL CHECK (service_generation > 0),
    node_id TEXT NULL REFERENCES nodes(id) ON DELETE SET NULL,
    image TEXT NOT NULL,
    command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    projected_files_json JSONB NOT NULL DEFAULT '[]'::jsonb,
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
    UNIQUE (plan_id)
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

-- +goose Down
DROP TABLE IF EXISTS runtime_nodes;
DROP TABLE IF EXISTS execution_intents;
DROP TABLE IF EXISTS node_agent_session_tokens;
DROP TABLE IF EXISTS node_heartbeats;
DROP TABLE IF EXISTS nodes;
