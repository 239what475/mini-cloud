-- +goose Up
ALTER TABLE fleet_plane_statuses
    ADD COLUMN IF NOT EXISTS last_inventory_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS fleet_plane_runtime_inventory_states (
    plane_id TEXT PRIMARY KEY REFERENCES fleet_planes(id) ON DELETE CASCADE,
    sync_version BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    nodes_total INTEGER NOT NULL,
    nodes_ready INTEGER NOT NULL,
    cpu_milli_capacity INTEGER NOT NULL,
    cpu_milli_allocated INTEGER NOT NULL,
    memory_mi_capacity INTEGER NOT NULL,
    memory_mi_allocated INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_plane_runtime_nodes (
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    node_epoch BIGINT NOT NULL DEFAULT 1,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    status TEXT NOT NULL,
    schedulable BOOLEAN NOT NULL,
    cpu_milli_capacity INTEGER NOT NULL,
    cpu_milli_allocated INTEGER NOT NULL,
    memory_mi_capacity INTEGER NOT NULL,
    memory_mi_allocated INTEGER NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plane_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_fleet_plane_runtime_nodes_plane_status
    ON fleet_plane_runtime_nodes (plane_id, status, schedulable);

CREATE TABLE IF NOT EXISTS fleet_service_cell_assignments (
    service_id TEXT NOT NULL,
    cell_key TEXT NOT NULL,
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE CASCADE,
    target_node_id TEXT NOT NULL,
    target_node_epoch BIGINT NOT NULL DEFAULT 1,
    inventory_version BIGINT NOT NULL,
    cpu_milli_reserved INTEGER NOT NULL,
    memory_mi_reserved INTEGER NOT NULL,
    state TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_id, cell_key),
    CONSTRAINT fleet_service_cell_assignments_cell_fk
        FOREIGN KEY (service_id, cell_key)
        REFERENCES fleet_service_cells(service_id, cell_key)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_fleet_service_cell_assignments_plane_node
    ON fleet_service_cell_assignments (plane_id, target_node_id, state);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_cell_assignments_plane_node;
DROP TABLE IF EXISTS fleet_service_cell_assignments;

DROP INDEX IF EXISTS idx_fleet_plane_runtime_nodes_plane_status;
DROP TABLE IF EXISTS fleet_plane_runtime_nodes;
DROP TABLE IF EXISTS fleet_plane_runtime_inventory_states;

ALTER TABLE fleet_plane_statuses
    DROP COLUMN IF EXISTS last_inventory_version;
