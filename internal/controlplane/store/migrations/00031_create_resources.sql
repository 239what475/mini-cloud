-- +goose Up
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
