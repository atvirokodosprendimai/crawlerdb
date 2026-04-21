-- +goose Up
ALTER TABLE pages ADD COLUMN content_path TEXT DEFAULT '';
ALTER TABLE pages ADD COLUMN content_size INTEGER NOT NULL DEFAULT 0;

-- +goose Down
SELECT 1;
