-- +goose Up
ALTER TABLE projects
    DROP COLUMN IF EXISTS quota_monthly_budget_cents;

-- +goose Down
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS quota_monthly_budget_cents INTEGER NOT NULL DEFAULT 20000 CHECK (quota_monthly_budget_cents > 0);
