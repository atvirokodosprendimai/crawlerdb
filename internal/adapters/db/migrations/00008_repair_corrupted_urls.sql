-- +goose Up
CREATE TEMP TABLE url_repair_pairs AS
SELECT
    bad.id AS bad_id,
    good.id AS good_id
FROM urls AS bad
JOIN urls AS good
    ON good.job_id = bad.job_id
   AND good.normalized = bad.normalized
   AND good.id <> bad.id
WHERE bad.url_hash = ''
  AND good.url_hash <> '';

UPDATE urls AS good
SET
    status = COALESCE(
        (
            SELECT bad.status
            FROM urls AS bad
            JOIN url_repair_pairs AS pairs ON pairs.bad_id = bad.id
            WHERE pairs.good_id = good.id
              AND bad.status IN ('done', 'blocked', 'error')
            LIMIT 1
        ),
        good.status
    ),
    retry_count = MAX(
        good.retry_count,
        COALESCE(
            (
                SELECT bad.retry_count
                FROM urls AS bad
                JOIN url_repair_pairs AS pairs ON pairs.bad_id = bad.id
                WHERE pairs.good_id = good.id
                LIMIT 1
            ),
            good.retry_count
        )
    ),
    revisit_at = COALESCE(
        good.revisit_at,
        (
            SELECT bad.revisit_at
            FROM urls AS bad
            JOIN url_repair_pairs AS pairs ON pairs.bad_id = bad.id
            WHERE pairs.good_id = good.id
            LIMIT 1
        )
    ),
    updated_at = MAX(
        good.updated_at,
        COALESCE(
            (
                SELECT bad.updated_at
                FROM urls AS bad
                JOIN url_repair_pairs AS pairs ON pairs.bad_id = bad.id
                WHERE pairs.good_id = good.id
                LIMIT 1
            ),
            good.updated_at
        )
    )
WHERE good.id IN (SELECT good_id FROM url_repair_pairs);

UPDATE pages
SET url_id = (
    SELECT good_id
    FROM url_repair_pairs
    WHERE bad_id = pages.url_id
)
WHERE url_id IN (SELECT bad_id FROM url_repair_pairs);

UPDATE antibot_events
SET url_id = (
    SELECT good_id
    FROM url_repair_pairs
    WHERE bad_id = antibot_events.url_id
)
WHERE url_id IN (SELECT bad_id FROM url_repair_pairs);

DELETE FROM urls
WHERE id IN (SELECT bad_id FROM url_repair_pairs);

DROP TABLE url_repair_pairs;

-- +goose Down
SELECT 1;
