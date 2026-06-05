-- +goose Up
-- control-plane runtime node pool policy was removed from the CaaS demo.
SELECT 1;

-- +goose Down
SELECT 1;
