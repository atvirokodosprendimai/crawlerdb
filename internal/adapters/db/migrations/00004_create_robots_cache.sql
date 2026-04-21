-- +goose Up
CREATE TABLE robots_cache (
    domain      TEXT PRIMARY KEY,
    content     TEXT NOT NULL,
    parsed      TEXT NOT NULL,
    fetched_at  DATETIME NOT NULL,
    expires_at  DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS robots_cache;
