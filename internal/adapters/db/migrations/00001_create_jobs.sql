-- +goose Up
CREATE TABLE jobs (
    id          TEXT PRIMARY KEY,
    seed_url    TEXT NOT NULL,
    config      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    stats       TEXT DEFAULT '{}',
    error       TEXT DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    started_at  DATETIME,
    finished_at DATETIME
);

CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_created_at ON jobs(created_at);

-- +goose Down
DROP TABLE IF EXISTS jobs;
