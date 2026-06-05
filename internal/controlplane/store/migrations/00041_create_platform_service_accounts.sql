-- +goose Up
-- platform service accounts were removed; the demo keeps a single admin token.
SELECT 1;

-- +goose Down
SELECT 1;
