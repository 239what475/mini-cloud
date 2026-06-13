-- +goose Up
CREATE TABLE IF NOT EXISTS control_events (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_control_events_created_at
    ON control_events (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_control_events_action_created_at
    ON control_events (action, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS planes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    grpc_endpoint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plane_statuses (
    plane_id TEXT PRIMARY KEY REFERENCES planes(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TIMESTAMPTZ NULL,
    last_sync_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS service_dns_records (
    plane_id TEXT NOT NULL REFERENCES planes(id) ON DELETE CASCADE,
    service_id TEXT NOT NULL,
    host TEXT NOT NULL,
    record_type TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plane_id, service_id, host, record_type, purpose)
);

CREATE INDEX IF NOT EXISTS idx_service_dns_records_host_type
    ON service_dns_records (host, record_type);

-- +goose Down
DROP INDEX IF EXISTS idx_service_dns_records_host_type;
DROP TABLE IF EXISTS service_dns_records;
DROP TABLE IF EXISTS plane_statuses;
DROP TABLE IF EXISTS planes;
DROP INDEX IF EXISTS idx_control_events_action_created_at;
DROP INDEX IF EXISTS idx_control_events_created_at;
DROP TABLE IF EXISTS control_events;
