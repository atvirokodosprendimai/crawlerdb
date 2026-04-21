-- +goose Up
CREATE TABLE pages (
    id              TEXT PRIMARY KEY,
    url_id          TEXT NOT NULL REFERENCES urls(id),
    job_id          TEXT NOT NULL REFERENCES jobs(id),
    http_status     INTEGER,
    content_type    TEXT DEFAULT '',
    headers         TEXT DEFAULT '{}',
    title           TEXT DEFAULT '',
    meta_tags       TEXT DEFAULT '{}',
    html_body       TEXT DEFAULT '',
    text_content    TEXT DEFAULT '',
    structured_data TEXT DEFAULT '[]',
    links           TEXT DEFAULT '[]',
    fetch_duration  INTEGER DEFAULT 0,
    fetched_at      DATETIME NOT NULL,
    created_at      DATETIME NOT NULL
);

CREATE INDEX idx_pages_url_id ON pages(url_id);
CREATE INDEX idx_pages_job_id ON pages(job_id);

-- +goose Down
DROP TABLE IF EXISTS pages;
