-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_files_json JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE fleet_services
    DROP COLUMN IF EXISTS spec_files_json;
