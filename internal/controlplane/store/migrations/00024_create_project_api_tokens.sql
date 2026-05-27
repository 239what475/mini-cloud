-- +goose Up
CREATE TABLE IF NOT EXISTS project_api_tokens (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_controlplane_project_api_tokens_project_created_at
    ON project_api_tokens (project_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_controlplane_project_api_tokens_project_created_at;
DROP TABLE IF EXISTS project_api_tokens;
