-- +goose Up
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS quota_max_services INTEGER NOT NULL DEFAULT 5 CHECK (quota_max_services > 0),
    ADD COLUMN IF NOT EXISTS quota_cpu_milli INTEGER NOT NULL DEFAULT 4000 CHECK (quota_cpu_milli > 0),
    ADD COLUMN IF NOT EXISTS quota_memory_mi INTEGER NOT NULL DEFAULT 8192 CHECK (quota_memory_mi > 0),
    ADD COLUMN IF NOT EXISTS quota_monthly_budget_cents INTEGER NOT NULL DEFAULT 20000 CHECK (quota_monthly_budget_cents > 0);

-- +goose Down
ALTER TABLE projects
    DROP COLUMN IF EXISTS quota_monthly_budget_cents,
    DROP COLUMN IF EXISTS quota_memory_mi,
    DROP COLUMN IF EXISTS quota_cpu_milli,
    DROP COLUMN IF EXISTS quota_max_services;
