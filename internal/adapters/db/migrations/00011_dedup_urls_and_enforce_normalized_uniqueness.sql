-- +goose Up
CREATE TEMP TABLE url_dedup_map AS
WITH ranked AS (
    SELECT
        id,
        job_id,
        normalized,
        FIRST_VALUE(id) OVER (
            PARTITION BY job_id, normalized
            ORDER BY
                CASE status
                    WHEN 'done' THEN 0
                    WHEN 'blocked' THEN 1
                    WHEN 'error' THEN 2
                    WHEN 'crawling' THEN 3
                    ELSE 4
                END,
                CASE WHEN url_hash = '' THEN 1 ELSE 0 END,
                depth ASC,
                created_at ASC,
                id ASC
        ) AS keep_id
    FROM urls
)
SELECT id AS drop_id, keep_id
FROM ranked
WHERE id <> keep_id;

UPDATE urls AS keep
SET
    url_hash = COALESCE(
        NULLIF(keep.url_hash, ''),
        (
            SELECT NULLIF(dupe.url_hash, '')
            FROM urls AS dupe
            JOIN url_dedup_map AS map ON map.drop_id = dupe.id
            WHERE map.keep_id = keep.id
            ORDER BY dupe.created_at ASC, dupe.id ASC
            LIMIT 1
        ),
        keep.url_hash
    ),
    depth = MIN(
        keep.depth,
        COALESCE(
            (
                SELECT MIN(dupe.depth)
                FROM urls AS dupe
                JOIN url_dedup_map AS map ON map.drop_id = dupe.id
                WHERE map.keep_id = keep.id
            ),
            keep.depth
        )
    ),
    retry_count = MAX(
        keep.retry_count,
        COALESCE(
            (
                SELECT MAX(dupe.retry_count)
                FROM urls AS dupe
                JOIN url_dedup_map AS map ON map.drop_id = dupe.id
                WHERE map.keep_id = keep.id
            ),
            keep.retry_count
        )
    ),
    last_error = COALESCE(
        NULLIF(keep.last_error, ''),
        (
            SELECT NULLIF(dupe.last_error, '')
            FROM urls AS dupe
            JOIN url_dedup_map AS map ON map.drop_id = dupe.id
            WHERE map.keep_id = keep.id
            ORDER BY dupe.updated_at DESC, dupe.id ASC
            LIMIT 1
        ),
        keep.last_error
    ),
    revisit_at = COALESCE(
        keep.revisit_at,
        (
            SELECT MAX(dupe.revisit_at)
            FROM urls AS dupe
            JOIN url_dedup_map AS map ON map.drop_id = dupe.id
            WHERE map.keep_id = keep.id
        )
    ),
    found_on = COALESCE(
        NULLIF(keep.found_on, ''),
        (
            SELECT NULLIF(dupe.found_on, '')
            FROM urls AS dupe
            JOIN url_dedup_map AS map ON map.drop_id = dupe.id
            WHERE map.keep_id = keep.id
            ORDER BY dupe.created_at ASC, dupe.id ASC
            LIMIT 1
        ),
        keep.found_on
    ),
    created_at = MIN(
        keep.created_at,
        COALESCE(
            (
                SELECT MIN(dupe.created_at)
                FROM urls AS dupe
                JOIN url_dedup_map AS map ON map.drop_id = dupe.id
                WHERE map.keep_id = keep.id
            ),
            keep.created_at
        )
    ),
    updated_at = MAX(
        keep.updated_at,
        COALESCE(
            (
                SELECT MAX(dupe.updated_at)
                FROM urls AS dupe
                JOIN url_dedup_map AS map ON map.drop_id = dupe.id
                WHERE map.keep_id = keep.id
            ),
            keep.updated_at
        )
    )
WHERE keep.id IN (SELECT DISTINCT keep_id FROM url_dedup_map);

UPDATE pages
SET url_id = (
    SELECT keep_id
    FROM url_dedup_map
    WHERE drop_id = pages.url_id
)
WHERE url_id IN (SELECT drop_id FROM url_dedup_map);

UPDATE antibot_events
SET url_id = (
    SELECT keep_id
    FROM url_dedup_map
    WHERE drop_id = antibot_events.url_id
)
WHERE url_id IN (SELECT drop_id FROM url_dedup_map);

DELETE FROM urls
WHERE id IN (SELECT drop_id FROM url_dedup_map);

DROP TABLE url_dedup_map;

CREATE UNIQUE INDEX IF NOT EXISTS idx_urls_job_normalized ON urls(job_id, normalized);

-- +goose Down
DROP INDEX IF EXISTS idx_urls_job_normalized;
