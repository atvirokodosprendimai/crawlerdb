-- +goose Up
CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'online',
    pool_size INTEGER NOT NULL DEFAULT 10,
    last_heartbeat DATETIME NOT NULL,
    started_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_workers_status ON workers(status);
CREATE INDEX idx_workers_last_heartbeat ON workers(last_heartbeat);

-- +goose Down
DROP TABLE IF EXISTS workers;
