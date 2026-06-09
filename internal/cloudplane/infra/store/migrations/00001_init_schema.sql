-- +goose Up
-- 当前仍处于开发期，cloud-plane 不保留旧 schema 的升级历史。
-- 这个 baseline 直接描述当前代码需要的完整数据库结构；已有开发库需要重建。

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    name TEXT NOT NULL,
    private_ip TEXT NOT NULL DEFAULT '',
    public_ip TEXT NOT NULL DEFAULT '',
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
    session_token_prefix TEXT NOT NULL DEFAULT '',
    session_token_hash TEXT NULL UNIQUE,
    session_last_used_at TIMESTAMPTZ NULL,
    session_expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, instance_id)
);

CREATE INDEX IF NOT EXISTS idx_nodes_status_created_at
    ON nodes (status, created_at ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_nodes_session_last_used_at
    ON nodes (session_last_used_at);

CREATE INDEX IF NOT EXISTS idx_nodes_session_expires_at
    ON nodes (session_expires_at);

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

-- +goose Down
DROP TABLE IF EXISTS execution_intents;
DROP TABLE IF EXISTS nodes;
