-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_plane_runtime_config_states (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    fingerprint TEXT NOT NULL,
    summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS fleet_plane_runtime_config_states;
