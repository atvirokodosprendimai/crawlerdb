-- +goose Up
CREATE TABLE urls (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES jobs(id),
    raw_url     TEXT NOT NULL,
    normalized  TEXT NOT NULL,
    url_hash    TEXT NOT NULL,
    depth       INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    revisit_at  DATETIME,
    found_on    TEXT DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    UNIQUE(job_id, url_hash)
);

CREATE INDEX idx_urls_job_status ON urls(job_id, status);
CREATE INDEX idx_urls_job_hash ON urls(job_id, url_hash);

-- +goose Down
DROP TABLE IF EXISTS urls;
