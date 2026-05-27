-- +goose Up
CREATE TABLE IF NOT EXISTS project_config_sets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_config_sets_project_name_key UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS project_secret_sets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_secret_sets_project_name_key UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS project_registry_credentials (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    server TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_registry_credentials_project_name_key UNIQUE (project_id, name)
);

-- +goose Down
DROP TABLE IF EXISTS project_registry_credentials;
DROP TABLE IF EXISTS project_secret_sets;
DROP TABLE IF EXISTS project_config_sets;
