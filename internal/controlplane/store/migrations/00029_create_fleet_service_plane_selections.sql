-- +goose Up
-- service cell selection was removed from the CaaS demo.
SELECT 1;

-- +goose Down
SELECT 1;
