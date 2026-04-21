-- +goose Up
CREATE TABLE IF NOT EXISTS domain_assignments (
    id TEXT PRIMARY KEY,
    worker_id TEXT NOT NULL REFERENCES workers(id),
    job_id TEXT NOT NULL REFERENCES jobs(id),
    domain TEXT NOT NULL,
    concurrency INTEGER NOT NULL DEFAULT 2,
    active_count INTEGER NOT NULL DEFAULT 0,
    assigned_at DATETIME NOT NULL,
    released_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_domain_assignments_worker ON domain_assignments(worker_id) WHERE released_at IS NULL;
CREATE INDEX idx_domain_assignments_domain ON domain_assignments(job_id, domain) WHERE released_at IS NULL;
CREATE UNIQUE INDEX idx_domain_assignments_active ON domain_assignments(job_id, domain) WHERE released_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS domain_assignments;
