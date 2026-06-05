-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_provider TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_region TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_pinned_plane_id TEXT NULL;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_instance_class TEXT NOT NULL DEFAULT 'small';

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fleet_services_pinned_plane_fk'
    ) THEN
        ALTER TABLE fleet_services
            ADD CONSTRAINT fleet_services_pinned_plane_fk
            FOREIGN KEY (spec_pinned_plane_id) REFERENCES fleet_planes(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
    END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS fleet_service_placements (
    service_id TEXT PRIMARY KEY REFERENCES fleet_services(id) ON DELETE CASCADE,
    plane_id TEXT NOT NULL REFERENCES fleet_planes(id) ON DELETE RESTRICT,
    remote_status TEXT NOT NULL DEFAULT '',
    remote_healthy BOOLEAN NOT NULL DEFAULT false,
    remote_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_fleet_service_placements_plane
    ON fleet_service_placements (plane_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_placements_plane;
DROP TABLE IF EXISTS fleet_service_placements;
ALTER TABLE fleet_services DROP CONSTRAINT IF EXISTS fleet_services_pinned_plane_fk;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_instance_class;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_pinned_plane_id;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_region;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_provider;
