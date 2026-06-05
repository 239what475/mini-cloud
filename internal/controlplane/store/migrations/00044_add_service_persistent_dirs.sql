-- +goose Up
-- persistentDirs was removed from the CaaS demo service model.
SELECT 1;

-- +goose Down
SELECT 1;
