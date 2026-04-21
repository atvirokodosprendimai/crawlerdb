package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
)

// PageRepository implements ports.PageRepository using GORM.
type PageRepository struct {
	db *gorm.DB
}

// NewPageRepository creates a new PageRepository.
func NewPageRepository(db *gorm.DB) *PageRepository {
	return &PageRepository{db: db}
}

func (r *PageRepository) Store(ctx context.Context, page *entities.Page) error {
	m, err := pageToModel(page)
	if err != nil {
		return fmt.Errorf("convert page to model: %w", err)
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *PageRepository) FindByURLID(ctx context.Context, urlID string) (*entities.Page, error) {
	var m PageModel
	if err := r.db.WithContext(ctx).Where("url_id = ?", urlID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToPage(&m)
}

func (r *PageRepository) FindByJobID(ctx context.Context, jobID string, limit, offset int) ([]*entities.Page, error) {
	var models []PageModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&models).Error; err != nil {
		return nil, err
	}
	pages := make([]*entities.Page, 0, len(models))
	for _, m := range models {
		p, err := modelToPage(&m)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}
