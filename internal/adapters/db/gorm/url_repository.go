package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// URLRepository implements ports.URLRepository using GORM.
type URLRepository struct {
	db *gorm.DB
}

// NewURLRepository creates a new URLRepository.
func NewURLRepository(db *gorm.DB) *URLRepository {
	return &URLRepository{db: db}
}

func (r *URLRepository) DedupeJobURLs(ctx context.Context, jobID string) (int64, error) {
	var deletedURLs int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			CREATE TEMP TABLE url_dedup_map AS
			WITH ranked AS (
				SELECT
					id,
					normalized,
					FIRST_VALUE(id) OVER (
						PARTITION BY normalized
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
				WHERE job_id = ?
			)
			SELECT id AS drop_id, keep_id
			FROM ranked
			WHERE id <> keep_id
		`, jobID).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			UPDATE urls AS keep
			SET
				url_hash = COALESCE(NULLIF(keep.url_hash, ''), (
					SELECT NULLIF(dupe.url_hash, '')
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
					ORDER BY dupe.created_at ASC, dupe.id ASC
					LIMIT 1
				), keep.url_hash),
				depth = MIN(keep.depth, COALESCE((
					SELECT MIN(dupe.depth)
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
				), keep.depth)),
				retry_count = MAX(keep.retry_count, COALESCE((
					SELECT MAX(dupe.retry_count)
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
				), keep.retry_count)),
				last_error = COALESCE(NULLIF(keep.last_error, ''), (
					SELECT NULLIF(dupe.last_error, '')
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
					ORDER BY dupe.updated_at DESC, dupe.id ASC
					LIMIT 1
				), keep.last_error),
				revisit_at = COALESCE(keep.revisit_at, (
					SELECT MAX(dupe.revisit_at)
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
				)),
				found_on = COALESCE(NULLIF(keep.found_on, ''), (
					SELECT NULLIF(dupe.found_on, '')
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
					ORDER BY dupe.created_at ASC, dupe.id ASC
					LIMIT 1
				), keep.found_on),
				created_at = MIN(keep.created_at, COALESCE((
					SELECT MIN(dupe.created_at)
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
				), keep.created_at)),
				updated_at = MAX(keep.updated_at, COALESCE((
					SELECT MAX(dupe.updated_at)
					FROM urls AS dupe
					JOIN url_dedup_map AS map ON map.drop_id = dupe.id
					WHERE map.keep_id = keep.id
				), keep.updated_at))
			WHERE keep.id IN (SELECT DISTINCT keep_id FROM url_dedup_map)
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			UPDATE pages
			SET url_id = (
				SELECT keep_id
				FROM url_dedup_map
				WHERE drop_id = pages.url_id
			)
			WHERE url_id IN (SELECT drop_id FROM url_dedup_map)
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			CREATE TEMP TABLE page_dedup_map AS
			WITH ranked AS (
				SELECT
					id,
					url_id,
					FIRST_VALUE(id) OVER (
						PARTITION BY url_id
						ORDER BY
							CASE WHEN content_path <> '' THEN 0 ELSE 1 END,
							fetched_at DESC,
							created_at DESC,
							id ASC
					) AS keep_id
				FROM pages
				WHERE job_id = ?
			)
			SELECT id AS drop_id, keep_id
			FROM ranked
			WHERE id <> keep_id
		`, jobID).Error; err != nil {
			return err
		}

		if err := tx.Exec(`
			UPDATE antibot_events
			SET url_id = (
				SELECT keep_id
				FROM url_dedup_map
				WHERE drop_id = antibot_events.url_id
			)
			WHERE url_id IN (SELECT drop_id FROM url_dedup_map)
		`).Error; err != nil {
			return err
		}

		if err := tx.Exec(`DELETE FROM pages WHERE id IN (SELECT drop_id FROM page_dedup_map)`).Error; err != nil {
			return err
		}

		result := tx.Exec(`DELETE FROM urls WHERE id IN (SELECT drop_id FROM url_dedup_map)`)
		if result.Error != nil {
			return result.Error
		}
		deletedURLs = result.RowsAffected

		if err := tx.Exec(`DROP TABLE page_dedup_map`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DROP TABLE url_dedup_map`).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deletedURLs, nil
}

func (r *URLRepository) Enqueue(ctx context.Context, url *entities.CrawlURL) error {
	m := urlToModel(url)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "job_id"}, {Name: "normalized"}},
		DoUpdates: clause.Assignments(map[string]any{
			"raw_url":  gorm.Expr("CASE WHEN raw_url = '' THEN excluded.raw_url ELSE raw_url END"),
			"url_hash": gorm.Expr("CASE WHEN url_hash = '' THEN excluded.url_hash ELSE url_hash END"),
			"depth":    gorm.Expr("MIN(depth, excluded.depth)"),
			"found_on": gorm.Expr("CASE WHEN found_on = '' THEN excluded.found_on ELSE found_on END"),
		}),
	}).Create(m)
	return result.Error
}

func (r *URLRepository) EnqueueBatch(ctx context.Context, urls []*entities.CrawlURL) error {
	if len(urls) == 0 {
		return nil
	}
	models := make([]*URLModel, len(urls))
	for i, u := range urls {
		models[i] = urlToModel(u)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "job_id"}, {Name: "normalized"}},
		DoUpdates: clause.Assignments(map[string]any{
			"raw_url":  gorm.Expr("CASE WHEN raw_url = '' THEN excluded.raw_url ELSE raw_url END"),
			"url_hash": gorm.Expr("CASE WHEN url_hash = '' THEN excluded.url_hash ELSE url_hash END"),
			"depth":    gorm.Expr("MIN(depth, excluded.depth)"),
			"found_on": gorm.Expr("CASE WHEN found_on = '' THEN excluded.found_on ELSE found_on END"),
		}),
	}).CreateInBatches(models, 100).Error
}

// Claim atomically transitions up to `limit` pending URLs to crawling status.
func (r *URLRepository) Claim(ctx context.Context, jobID string, limit int) ([]*entities.CrawlURL, error) {
	var models []URLModel

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Select pending URLs.
		if err := tx.Where("job_id = ? AND status = ?", jobID, string(entities.URLStatusPending)).
			Order("depth ASC, created_at ASC").
			Limit(limit).
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}

		// Update status to crawling.
		ids := make([]string, len(models))
		for i, m := range models {
			ids[i] = m.ID
		}
		now := time.Now().UTC()
		return tx.Model(&URLModel{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":     string(entities.URLStatusCrawling),
				"updated_at": now,
			}).Error
	})

	if err != nil {
		return nil, fmt.Errorf("claim URLs: %w", err)
	}

	result := make([]*entities.CrawlURL, len(models))
	for i, m := range models {
		u := modelToURL(&m)
		u.Status = entities.URLStatusCrawling
		result[i] = u
	}
	return result, nil
}

func (r *URLRepository) ClaimByIDs(ctx context.Context, jobID string, ids []string) ([]*entities.CrawlURL, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var models []URLModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("job_id = ? AND status = ? AND id IN ?", jobID, string(entities.URLStatusPending), ids).
			Order("depth ASC, created_at ASC").
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}

		claimedIDs := make([]string, len(models))
		for i, m := range models {
			claimedIDs[i] = m.ID
		}
		now := time.Now().UTC()
		return tx.Model(&URLModel{}).
			Where("job_id = ? AND status = ? AND id IN ?", jobID, string(entities.URLStatusPending), claimedIDs).
			Updates(map[string]any{
				"status":     string(entities.URLStatusCrawling),
				"updated_at": now,
			}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("claim URLs by IDs: %w", err)
	}

	result := make([]*entities.CrawlURL, len(models))
	for i, m := range models {
		u := modelToURL(&m)
		u.Status = entities.URLStatusCrawling
		result[i] = u
	}
	return result, nil
}

func (r *URLRepository) RequeueCrawlingByDomain(ctx context.Context, jobID, domain string) (int64, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("job_id = ? AND status = ?", jobID, string(entities.URLStatusCrawling)).
		Where(`
			CASE
				WHEN INSTR(SUBSTR(normalized, INSTR(normalized, '://') + 3), '/') > 0
				THEN SUBSTR(
					SUBSTR(normalized, INSTR(normalized, '://') + 3),
					1,
					INSTR(SUBSTR(normalized, INSTR(normalized, '://') + 3), '/') - 1
				)
				ELSE SUBSTR(normalized, INSTR(normalized, '://') + 3)
			END = ?
		`, domain).
		Updates(map[string]any{
			"status":     string(entities.URLStatusPending),
			"updated_at": now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *URLRepository) RequeueCrawlingByJob(ctx context.Context, jobID string) (int64, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("job_id = ? AND status = ?", jobID, string(entities.URLStatusCrawling)).
		Updates(map[string]any{
			"status":     string(entities.URLStatusPending),
			"updated_at": now,
			"last_error": "manually reset from crawling",
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *URLRepository) RequeueTimedOutCrawling(ctx context.Context, before time.Time) (int64, error) {
	requeued, _, err := r.RequeueTimedOutCrawlingWithLimit(ctx, before, 0)
	return requeued, err
}

func (r *URLRepository) RequeueTimedOutCrawlingWithLimit(ctx context.Context, before time.Time, maxRetries int) (int64, int64, error) {
	now := time.Now().UTC()
	timeoutMsg := fmt.Sprintf("crawl timeout after %s", now.Sub(before).Round(time.Second))

	var requeued int64
	var failed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if maxRetries > 0 {
			result := tx.Model(&URLModel{}).
				Where("status = ? AND updated_at < ? AND retry_count + 1 >= ?", string(entities.URLStatusCrawling), before, maxRetries).
				Updates(map[string]any{
					"status":      string(entities.URLStatusError),
					"retry_count": gorm.Expr("retry_count + 1"),
					"updated_at":  now,
					"last_error":  timeoutMsg + "; max retries exceeded",
				})
			if result.Error != nil {
				return result.Error
			}
			failed = result.RowsAffected
		}

		query := tx.Model(&URLModel{}).
			Where("status = ? AND updated_at < ?", string(entities.URLStatusCrawling), before)
		if maxRetries > 0 {
			query = query.Where("retry_count + 1 < ?", maxRetries)
		}
		result := query.Updates(map[string]any{
			"status":      string(entities.URLStatusPending),
			"retry_count": gorm.Expr("retry_count + 1"),
			"updated_at":  now,
			"last_error":  timeoutMsg + "; requeued",
		})
		if result.Error != nil {
			return result.Error
		}
		requeued = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return requeued, failed, nil
}

func (r *URLRepository) FailPendingOverRetryLimit(ctx context.Context, jobID string, maxRetries int) (int64, error) {
	if maxRetries <= 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("job_id = ? AND status = ? AND retry_count >= ?", jobID, string(entities.URLStatusPending), maxRetries).
		Updates(map[string]any{
			"status":     string(entities.URLStatusError),
			"updated_at": now,
			"last_error": fmt.Sprintf("max retries exceeded (%d); skipped before dispatch", maxRetries),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *URLRepository) RequeueDueRevisits(ctx context.Context, before time.Time) (int64, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("status = ? AND revisit_at IS NOT NULL AND revisit_at <= ?", string(entities.URLStatusDone), before).
		Updates(map[string]any{
			"status":      string(entities.URLStatusPending),
			"updated_at":  now,
			"revisit_at":  nil,
			"last_error":  "",
			"retry_count": 0,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *URLRepository) RequeueJobForRevisit(ctx context.Context, jobID string) (int64, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("job_id = ? AND status IN ?", jobID, []string{
			string(entities.URLStatusDone),
			string(entities.URLStatusError),
			string(entities.URLStatusBlocked),
		}).
		Updates(map[string]any{
			"status":      string(entities.URLStatusPending),
			"updated_at":  now,
			"revisit_at":  nil,
			"last_error":  "",
			"retry_count": 0,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *URLRepository) Complete(ctx context.Context, url *entities.CrawlURL) error {
	updates := map[string]any{
		"status":      string(url.Status),
		"retry_count": url.RetryCount,
		"last_error":  url.LastError,
		"updated_at":  url.UpdatedAt,
	}
	if !url.RevisitAt.IsZero() {
		updates["revisit_at"] = url.RevisitAt
	}

	result := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Where("id = ?", url.ID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *URLRepository) FindByHash(ctx context.Context, jobID, urlHash string) (*entities.CrawlURL, error) {
	var m URLModel
	if err := r.db.WithContext(ctx).Where("job_id = ? AND url_hash = ?", jobID, urlHash).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToURL(&m), nil
}

func (r *URLRepository) FindPending(ctx context.Context, jobID string, limit int) ([]*entities.CrawlURL, error) {
	var models []URLModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ? AND status = ?", jobID, string(entities.URLStatusPending)).
		Order("depth ASC, created_at ASC").
		Limit(limit).
		Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*entities.CrawlURL, len(models))
	for i, m := range models {
		result[i] = modelToURL(&m)
	}
	return result, nil
}

func (r *URLRepository) FindByJobID(ctx context.Context, jobID string, limit, offset int) ([]*entities.CrawlURL, error) {
	var models []URLModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("depth ASC, created_at ASC").
		Limit(limit).Offset(offset).
		Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*entities.CrawlURL, len(models))
	for i, m := range models {
		result[i] = modelToURL(&m)
	}
	return result, nil
}

func (r *URLRepository) FindByJobIDAndStatuses(ctx context.Context, jobID string, statuses []entities.URLStatus, limit, offset int) ([]*entities.CrawlURL, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	statusVals := make([]string, len(statuses))
	for i, status := range statuses {
		statusVals[i] = string(status)
	}

	var models []URLModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ? AND status IN ?", jobID, statusVals).
		Order("updated_at DESC, created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*entities.CrawlURL, len(models))
	for i, m := range models {
		result[i] = modelToURL(&m)
	}
	return result, nil
}

func (r *URLRepository) CountByStatus(ctx context.Context, jobID string) (map[entities.URLStatus]int, error) {
	type statusCount struct {
		Status string
		Count  int
	}
	var counts []statusCount
	if err := r.db.WithContext(ctx).
		Model(&URLModel{}).
		Select("status, count(*) as count").
		Where("job_id = ?", jobID).
		Group("status").
		Scan(&counts).Error; err != nil {
		return nil, err
	}

	result := make(map[entities.URLStatus]int)
	for _, c := range counts {
		result[entities.URLStatus(c.Status)] = c.Count
	}
	return result, nil
}
