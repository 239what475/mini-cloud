-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_services (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    spec_exposure TEXT NOT NULL,
    spec_image TEXT NOT NULL,
    spec_command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    spec_default_port INTEGER NOT NULL,
    spec_readiness_path TEXT NOT NULL,
    spec_env_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_config_set_id TEXT NOT NULL DEFAULT '',
    spec_secret_set_id TEXT NOT NULL DEFAULT '',
    spec_registry_credential_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fleet_services_project_name_key UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_fleet_services_project_created_at
    ON fleet_services (project_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_services_project_created_at;
DROP TABLE IF EXISTS fleet_services;
