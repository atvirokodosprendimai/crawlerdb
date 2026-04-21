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

func (r *URLRepository) Enqueue(ctx context.Context, url *entities.CrawlURL) error {
	m := urlToModel(url)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(m)
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
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(models, 100).Error
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
