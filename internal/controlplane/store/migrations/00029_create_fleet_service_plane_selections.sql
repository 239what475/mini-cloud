-- +goose Up
CREATE TABLE IF NOT EXISTS fleet_service_cells (
    service_id TEXT NOT NULL REFERENCES fleet_services(id) ON DELETE CASCADE,
    cell_key TEXT NOT NULL,
    role TEXT NOT NULL,
    spec_provider TEXT NOT NULL,
    spec_region TEXT NOT NULL,
    spec_pinned_plane_id TEXT NULL,
    spec_replicas INTEGER NOT NULL,
    spec_instance_class TEXT NOT NULL,
    status_desired_state TEXT NOT NULL DEFAULT 'active',
    status_observed_generation BIGINT NOT NULL DEFAULT 0,
    status_phase TEXT NOT NULL DEFAULT 'pending',
    status_healthy BOOLEAN NOT NULL DEFAULT false,
    status_message TEXT NOT NULL DEFAULT '',
    status_conditions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    status_last_reconciled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_id, cell_key),
    CONSTRAINT fleet_service_cells_pinned_plane_fk
        FOREIGN KEY (spec_pinned_plane_id) REFERENCES fleet_planes(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_fleet_service_cells_service_created_at
    ON fleet_service_cells (service_id, created_at ASC, cell_key ASC);

CREATE TABLE IF NOT EXISTS fleet_service_cell_placements (
    service_id TEXT NOT NULL,
    cell_key TEXT NOT NULL,
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE RESTRICT,
    remote_status TEXT NOT NULL DEFAULT '',
    remote_healthy BOOLEAN NOT NULL DEFAULT false,
    remote_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_id, cell_key),
    CONSTRAINT fleet_service_cell_placements_cell_fk
        FOREIGN KEY (service_id, cell_key) REFERENCES fleet_service_cells(service_id, cell_key) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_fleet_service_cell_placements_plane
    ON fleet_service_cell_placements (plane_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_cell_placements_plane;
DROP TABLE IF EXISTS fleet_service_cell_placements;
DROP INDEX IF EXISTS idx_fleet_service_cells_service_created_at;
DROP TABLE IF EXISTS fleet_service_cells;
