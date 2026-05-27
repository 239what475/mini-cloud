-- +goose Up
CREATE TABLE IF NOT EXISTS platform_service_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_controlplane_platform_service_accounts_created_at
    ON platform_service_accounts (created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_controlplane_platform_service_accounts_created_at;
DROP TABLE IF EXISTS platform_service_accounts;
