-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_run_json JSONB NOT NULL DEFAULT '{"phase":"pending","message":""}'::jsonb;

CREATE TABLE IF NOT EXISTS fleet_service_runs (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES fleet_services(id) ON DELETE CASCADE,
    generation BIGINT NOT NULL,
    plan_id TEXT NOT NULL,
    spec_json JSONB NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fleet_service_runs_service_generation_key UNIQUE (service_id, generation),
    CONSTRAINT fleet_service_runs_plan_key UNIQUE (plan_id)
);

CREATE INDEX IF NOT EXISTS idx_fleet_service_runs_service_created
    ON fleet_service_runs (service_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_runs_service_created;
DROP TABLE IF EXISTS fleet_service_runs;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_run_json;
