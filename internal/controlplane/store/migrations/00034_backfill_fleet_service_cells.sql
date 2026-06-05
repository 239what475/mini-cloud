-- +goose Up
-- service cell topology was removed from the CaaS demo.
SELECT 1;

-- +goose Down
SELECT 1;
