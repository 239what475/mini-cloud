-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_planes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    grpc_endpoint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_plane_statuses (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TIMESTAMPTZ NULL,
    last_sync_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_plane_capacity_snapshots (
    id TEXT PRIMARY KEY,
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE CASCADE,
    nodes_total INTEGER NOT NULL,
    nodes_ready INTEGER NOT NULL,
    services_total INTEGER NOT NULL,
    runs_total INTEGER NOT NULL,
    cpu_milli_capacity INTEGER NOT NULL,
    cpu_milli_allocated INTEGER NOT NULL,
    memory_mi_capacity INTEGER NOT NULL,
    memory_mi_allocated INTEGER NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_fleet_plane_capacity_snapshots_plane_captured_at
    ON fleet_plane_capacity_snapshots (plane_id, captured_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_plane_capacity_snapshots_plane_captured_at;
DROP TABLE IF EXISTS fleet_plane_capacity_snapshots;

DROP TABLE IF EXISTS fleet_plane_statuses;
DROP TABLE IF EXISTS fleet_planes;
