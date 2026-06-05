-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_run_json JSONB NOT NULL DEFAULT '{"phase":"pending","message":""}'::jsonb;

-- +goose Down
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_run_json;
