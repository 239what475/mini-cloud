-- +goose Up
ALTER TABLE fleet_services
    ADD COLUMN IF NOT EXISTS generation BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS status_desired_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS status_observed_generation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS status_phase TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS status_healthy BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS status_message TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status_conditions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS status_last_reconciled_at TIMESTAMPTZ NULL;

-- +goose Down
ALTER TABLE fleet_services
    DROP COLUMN IF EXISTS status_last_reconciled_at,
    DROP COLUMN IF EXISTS status_conditions_json,
    DROP COLUMN IF EXISTS status_message,
    DROP COLUMN IF EXISTS status_healthy,
    DROP COLUMN IF EXISTS status_phase,
    DROP COLUMN IF EXISTS status_observed_generation,
    DROP COLUMN IF EXISTS status_desired_state,
    DROP COLUMN IF EXISTS generation;
