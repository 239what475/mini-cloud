-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_provider TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_region TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_pinned_plane_id TEXT NULL;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_instance_class TEXT NOT NULL DEFAULT 'small';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_assigned_plane_id TEXT NULL;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_remote_status TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_remote_message TEXT NOT NULL DEFAULT '';

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

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fleet_services_assigned_plane_fk'
    ) THEN
        ALTER TABLE fleet_services
            ADD CONSTRAINT fleet_services_assigned_plane_fk
            FOREIGN KEY (status_assigned_plane_id) REFERENCES fleet_planes(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE fleet_services DROP CONSTRAINT IF EXISTS fleet_services_assigned_plane_fk;
ALTER TABLE fleet_services DROP CONSTRAINT IF EXISTS fleet_services_pinned_plane_fk;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_remote_message;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_remote_status;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_assigned_plane_id;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_instance_class;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_pinned_plane_id;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_region;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_provider;
