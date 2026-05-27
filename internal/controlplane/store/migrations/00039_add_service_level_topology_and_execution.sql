-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_provider TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_region TEXT NOT NULL DEFAULT '';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_pinned_plane_id TEXT NULL;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_replicas INTEGER NOT NULL DEFAULT 1;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_instance_class TEXT NOT NULL DEFAULT 'small';

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_revision_policy_json JSONB NOT NULL DEFAULT '{"strategy":"candidate"}'::jsonb;

ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS status_rollout_json JSONB NOT NULL DEFAULT '{"phase":"idle","message":"","stableRevisionID":"","candidateRevisionID":"","stableDesiredReplicas":0,"stableReadyReplicas":0,"stableAvailableReplicas":0,"candidateDesiredReplicas":0,"candidateReadyReplicas":0,"candidateAvailableReplicas":0}'::jsonb;

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

WITH ranked_cells AS (
    SELECT
        c.*,
        ROW_NUMBER() OVER (
            PARTITION BY c.service_id
            ORDER BY
                CASE c.role WHEN 'primary' THEN 0 WHEN 'standby' THEN 1 ELSE 2 END,
                c.created_at ASC,
                c.cell_key ASC
        ) AS row_num
    FROM fleet_service_cells c
)
UPDATE fleet_services AS s
SET
    spec_provider = rc.spec_provider,
    spec_region = rc.spec_region,
    spec_pinned_plane_id = rc.spec_pinned_plane_id,
    spec_replicas = rc.spec_replicas,
    spec_instance_class = rc.spec_instance_class,
    spec_revision_policy_json = COALESCE(rc.spec_revision_policy_json, s.spec_revision_policy_json),
    status_rollout_json = COALESCE(rc.status_rollout_json, s.status_rollout_json),
    updated_at = now()
FROM ranked_cells rc
WHERE s.id = rc.service_id
  AND rc.row_num = 1
  AND (s.spec_provider = '' OR s.spec_region = '');

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

WITH ranked_placements AS (
    SELECT
        p.*,
        ROW_NUMBER() OVER (
            PARTITION BY p.service_id
            ORDER BY
                CASE c.role WHEN 'primary' THEN 0 WHEN 'standby' THEN 1 ELSE 2 END,
                p.created_at ASC,
                p.cell_key ASC
        ) AS row_num
    FROM fleet_service_cell_placements p
    JOIN fleet_service_cells c
      ON c.service_id = p.service_id
     AND c.cell_key = p.cell_key
)
INSERT INTO fleet_service_placements (
    service_id,
    plane_id,
    remote_status,
    remote_healthy,
    remote_message,
    created_at,
    updated_at
)
SELECT
    rp.service_id,
    rp.plane_id,
    rp.remote_status,
    rp.remote_healthy,
    rp.remote_message,
    rp.created_at,
    rp.updated_at
FROM ranked_placements rp
WHERE rp.row_num = 1
ON CONFLICT (service_id) DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_service_placements_plane;
DROP TABLE IF EXISTS fleet_service_placements;
ALTER TABLE fleet_services DROP CONSTRAINT IF EXISTS fleet_services_pinned_plane_fk;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS status_rollout_json;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_revision_policy_json;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_instance_class;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_replicas;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_pinned_plane_id;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_region;
ALTER TABLE fleet_services DROP COLUMN IF EXISTS spec_provider;
