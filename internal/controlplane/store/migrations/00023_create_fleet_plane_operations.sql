-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_plane_operations (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('active', 'maintenance', 'draining')),
    reason TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO fleet_plane_operations (
    plane_id,
    state,
    reason
)
SELECT
    id,
    'active',
    ''
FROM fleet_planes
ON CONFLICT (plane_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS fleet_plane_operations;
