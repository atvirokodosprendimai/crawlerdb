-- +goose Up
CREATE TABLE antibot_events (
    id          TEXT PRIMARY KEY,
    url_id      TEXT NOT NULL REFERENCES urls(id),
    job_id      TEXT NOT NULL REFERENCES jobs(id),
    event_type  TEXT NOT NULL,
    provider    TEXT DEFAULT '',
    strategy    TEXT NOT NULL,
    resolved    BOOLEAN NOT NULL DEFAULT FALSE,
    details     TEXT DEFAULT '{}',
    created_at  DATETIME NOT NULL
);

CREATE INDEX idx_antibot_job_id ON antibot_events(job_id);
CREATE INDEX idx_antibot_url_id ON antibot_events(url_id);

-- +goose Down
DROP TABLE IF EXISTS antibot_events;
