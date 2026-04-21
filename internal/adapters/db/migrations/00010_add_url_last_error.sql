-- +goose Up
ALTER TABLE urls ADD COLUMN last_error TEXT DEFAULT '';

-- +goose Down
SELECT 1;
