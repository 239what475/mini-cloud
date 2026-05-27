-- +goose Up
ALTER TABLE IF EXISTS fleet_plane_bootstrap_tokens
    RENAME TO plane_southbound_tokens;

ALTER TABLE IF EXISTS plane_southbound_tokens
    RENAME COLUMN bootstrap_token TO southbound_token;

-- +goose Down
ALTER TABLE IF EXISTS plane_southbound_tokens
    RENAME COLUMN southbound_token TO bootstrap_token;

ALTER TABLE IF EXISTS plane_southbound_tokens
    RENAME TO fleet_plane_bootstrap_tokens;
