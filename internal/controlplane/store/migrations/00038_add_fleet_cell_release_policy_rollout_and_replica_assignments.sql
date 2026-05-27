-- +goose Up
ALTER TABLE fleet_service_cells
    ADD COLUMN IF NOT EXISTS spec_revision_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE fleet_service_cells
    ADD COLUMN IF NOT EXISTS status_rollout_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE fleet_service_cell_assignments
    ADD COLUMN IF NOT EXISTS replica_index INTEGER NOT NULL DEFAULT 0;

ALTER TABLE fleet_service_cell_assignments
    DROP CONSTRAINT IF EXISTS fleet_service_cell_assignments_pkey;

ALTER TABLE fleet_service_cell_assignments
    ADD CONSTRAINT fleet_service_cell_assignments_pkey
    PRIMARY KEY (service_id, cell_key, replica_index);

CREATE INDEX IF NOT EXISTS idx_fleet_service_cell_assignments_service_cell
    ON fleet_service_cell_assignments (service_id, cell_key, replica_index);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_cell_assignments_service_cell;

ALTER TABLE fleet_service_cell_assignments
    DROP CONSTRAINT IF EXISTS fleet_service_cell_assignments_pkey;

ALTER TABLE fleet_service_cell_assignments
    ADD CONSTRAINT fleet_service_cell_assignments_pkey
    PRIMARY KEY (service_id, cell_key);

ALTER TABLE fleet_service_cell_assignments
    DROP COLUMN IF EXISTS replica_index;

ALTER TABLE fleet_service_cells
    DROP COLUMN IF EXISTS status_rollout_json;

ALTER TABLE fleet_service_cells
    DROP COLUMN IF EXISTS spec_revision_policy_json;
