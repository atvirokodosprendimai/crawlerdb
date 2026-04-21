-- +goose Up
ALTER TABLE jobs ADD COLUMN delete_marked_at DATETIME;

CREATE INDEX idx_jobs_delete_marked_at ON jobs(delete_marked_at) WHERE delete_marked_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_delete_marked_at;
