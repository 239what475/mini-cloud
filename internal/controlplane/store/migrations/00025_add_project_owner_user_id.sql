-- +goose Up
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS owner_user_id TEXT;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM projects
        WHERE owner_user_id IS NULL OR btrim(owner_user_id) = ''
    ) THEN
        RAISE EXCEPTION 'projects.owner_user_id must be backfilled before applying 00025_add_project_owner_user_id';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE projects
    ALTER COLUMN owner_user_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_projects_owner_user_id
    ON projects (owner_user_id, created_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_projects_owner_user_id;

ALTER TABLE projects
    DROP COLUMN IF EXISTS owner_user_id;
