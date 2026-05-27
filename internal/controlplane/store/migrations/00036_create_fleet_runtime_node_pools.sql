-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_runtime_node_pools (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes (id) ON DELETE CASCADE,
    min_ready INTEGER NOT NULL,
    max_ready INTEGER NOT NULL,
    headroom_cpu_milli INTEGER NOT NULL,
    headroom_memory_mi INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS fleet_runtime_node_pools;
