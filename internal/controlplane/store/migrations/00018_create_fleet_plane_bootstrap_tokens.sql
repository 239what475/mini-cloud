-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_plane_bootstrap_tokens (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    bootstrap_token TEXT NOT NULL,
    last_verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS fleet_plane_bootstrap_tokens;
