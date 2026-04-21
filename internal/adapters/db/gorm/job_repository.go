package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
)

// JobRepository implements ports.JobRepository using GORM.
type JobRepository struct {
	db *gorm.DB
}

// NewJobRepository creates a new JobRepository.
func NewJobRepository(db *gorm.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Create(ctx context.Context, job *entities.Job) error {
	m, err := jobToModel(job)
	if err != nil {
		return fmt.Errorf("convert job to model: %w", err)
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *JobRepository) FindByID(ctx context.Context, id string) (*entities.Job, error) {
	var m JobModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToJob(&m)
}

func (r *JobRepository) Update(ctx context.Context, job *entities.Job) error {
	m, err := jobToModel(job)
	if err != nil {
		return fmt.Errorf("convert job to model: %w", err)
	}
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *JobRepository) List(ctx context.Context, limit, offset int) ([]*entities.Job, error) {
	var models []JobModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset).Find(&models).Error; err != nil {
		return nil, err
	}
	jobs := make([]*entities.Job, 0, len(models))
	for _, m := range models {
		j, err := modelToJob(&m)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (r *JobRepository) FindByStatus(ctx context.Context, status entities.JobStatus) ([]*entities.Job, error) {
	var models []JobModel
	if err := r.db.WithContext(ctx).Where("status = ?", string(status)).Find(&models).Error; err != nil {
		return nil, err
	}
	jobs := make([]*entities.Job, 0, len(models))
	for _, m := range models {
		j, err := modelToJob(&m)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
