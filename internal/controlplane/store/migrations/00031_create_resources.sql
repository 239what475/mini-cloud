-- +goose Up
CREATE TABLE IF NOT EXISTS config_sets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT config_sets_name_key UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS secret_sets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    values_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT secret_sets_name_key UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS registry_credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    server TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT registry_credentials_name_key UNIQUE (name)
);

-- +goose Down
DROP TABLE IF EXISTS registry_credentials;
DROP TABLE IF EXISTS secret_sets;
DROP TABLE IF EXISTS config_sets;
