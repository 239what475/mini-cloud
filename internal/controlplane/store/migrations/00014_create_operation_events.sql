-- +goose Up
CREATE TABLE IF NOT EXISTS operation_events (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    actor_kind TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    actor_label TEXT NOT NULL,
    actor_project_id TEXT NOT NULL DEFAULT '',
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    result TEXT NOT NULL,
    details_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_operation_events_created_at
    ON operation_events (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_operation_events_project_created_at
    ON operation_events (project_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_operation_events_action_created_at
    ON operation_events (action, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_operation_events_action_created_at;
DROP INDEX IF EXISTS idx_operation_events_project_created_at;
DROP INDEX IF EXISTS idx_operation_events_created_at;
DROP TABLE IF EXISTS operation_events;
