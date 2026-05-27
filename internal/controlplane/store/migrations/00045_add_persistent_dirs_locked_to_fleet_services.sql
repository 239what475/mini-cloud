-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS spec_persistent_dirs_locked BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE fleet_services AS s
SET spec_persistent_dirs_locked = TRUE
WHERE s.spec_persistent_dirs_locked = FALSE
    AND jsonb_typeof(s.spec_persistent_dirs_json) = 'array'
    AND jsonb_array_length(s.spec_persistent_dirs_json) > 0
    AND (
        EXISTS (
            SELECT 1
            FROM fleet_service_placements AS p
            WHERE p.service_id = s.id
        )
        OR COALESCE(s.status_rollout_json->>'stableRevisionID', '') <> ''
        OR COALESCE(s.status_rollout_json->>'candidateRevisionID', '') <> ''
    );

-- +goose Down
ALTER TABLE fleet_services
    DROP COLUMN IF EXISTS spec_persistent_dirs_locked;
