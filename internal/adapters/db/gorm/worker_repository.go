package store

import (
	"context"
	"errors"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
)

// WorkerRepository implements ports.WorkerRepository using GORM.
type WorkerRepository struct {
	db *gorm.DB
}

// NewWorkerRepository creates a new WorkerRepository.
func NewWorkerRepository(db *gorm.DB) *WorkerRepository {
	return &WorkerRepository{db: db}
}

func (r *WorkerRepository) Register(ctx context.Context, w *entities.Worker) error {
	m := &WorkerModel{
		ID:            w.ID,
		Hostname:      w.Hostname,
		Status:        string(w.Status),
		PoolSize:      w.PoolSize,
		LastHeartbeat: w.LastHeartbeat,
		StartedAt:     w.StartedAt,
		CreatedAt:     w.CreatedAt,
		UpdatedAt:     w.UpdatedAt,
	}
	// Upsert: on restart with same ID, update fields.
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *WorkerRepository) FindByID(ctx context.Context, id string) (*entities.Worker, error) {
	var m WorkerModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToWorker(&m), nil
}

func (r *WorkerRepository) UpdateHeartbeat(ctx context.Context, workerID string, t time.Time) error {
	return r.db.WithContext(ctx).
		Model(&WorkerModel{}).
		Where("id = ?", workerID).
		Updates(map[string]any{
			"last_heartbeat": t,
			"updated_at":     t,
			"status":         string(entities.WorkerStatusOnline),
		}).Error
}

func (r *WorkerRepository) ListOnline(ctx context.Context) ([]*entities.Worker, error) {
	var models []WorkerModel
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(entities.WorkerStatusOnline)).
		Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*entities.Worker, len(models))
	for i, m := range models {
		result[i] = modelToWorker(&m)
	}
	return result, nil
}

func (r *WorkerRepository) FindStale(ctx context.Context, ttl time.Duration) ([]*entities.Worker, error) {
	cutoff := time.Now().UTC().Add(-ttl)
	var models []WorkerModel
	if err := r.db.WithContext(ctx).
		Where("status = ? AND last_heartbeat < ?", string(entities.WorkerStatusOnline), cutoff).
		Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*entities.Worker, len(models))
	for i, m := range models {
		result[i] = modelToWorker(&m)
	}
	return result, nil
}

func (r *WorkerRepository) MarkOffline(ctx context.Context, workerID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&WorkerModel{}).
		Where("id = ?", workerID).
		Updates(map[string]any{
			"status":     string(entities.WorkerStatusOffline),
			"updated_at": now,
		}).Error
}

func modelToWorker(m *WorkerModel) *entities.Worker {
	return &entities.Worker{
		ID:            m.ID,
		Hostname:      m.Hostname,
		Status:        entities.WorkerStatus(m.Status),
		PoolSize:      m.PoolSize,
		LastHeartbeat: m.LastHeartbeat,
		StartedAt:     m.StartedAt,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}
