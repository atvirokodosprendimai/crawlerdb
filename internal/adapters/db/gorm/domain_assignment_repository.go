package store

import (
	"context"
	"errors"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"gorm.io/gorm"
)

// DomainAssignmentRepository implements ports.DomainAssignmentRepository using GORM.
type DomainAssignmentRepository struct {
	db *gorm.DB
}

// NewDomainAssignmentRepository creates a new DomainAssignmentRepository.
func NewDomainAssignmentRepository(db *gorm.DB) *DomainAssignmentRepository {
	return &DomainAssignmentRepository{db: db}
}

func (r *DomainAssignmentRepository) Assign(ctx context.Context, a *entities.DomainAssignment) error {
	m := assignmentToModel(a)
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *DomainAssignmentRepository) FindByWorker(ctx context.Context, workerID string) ([]*entities.DomainAssignment, error) {
	var models []DomainAssignmentModel
	if err := r.db.WithContext(ctx).
		Where("worker_id = ? AND released_at IS NULL", workerID).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return modelsToAssignments(models), nil
}

func (r *DomainAssignmentRepository) FindByDomain(ctx context.Context, jobID, domain string) (*entities.DomainAssignment, error) {
	var m DomainAssignmentModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ? AND domain = ? AND released_at IS NULL", jobID, domain).
		First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return modelToAssignment(&m), nil
}

func (r *DomainAssignmentRepository) Release(ctx context.Context, assignmentID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&DomainAssignmentModel{}).
		Where("id = ?", assignmentID).
		Updates(map[string]any{
			"released_at": now,
			"updated_at":  now,
		}).Error
}

func (r *DomainAssignmentRepository) ReleaseByWorker(ctx context.Context, workerID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&DomainAssignmentModel{}).
		Where("worker_id = ? AND released_at IS NULL", workerID).
		Updates(map[string]any{
			"released_at": now,
			"updated_at":  now,
		}).Error
}

func (r *DomainAssignmentRepository) ListActive(ctx context.Context, jobID string) ([]*entities.DomainAssignment, error) {
	var models []DomainAssignmentModel
	if err := r.db.WithContext(ctx).
		Where("job_id = ? AND released_at IS NULL", jobID).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return modelsToAssignments(models), nil
}

// FindUnassignedDomains returns domains that have pending URLs but no active assignment.
func (r *DomainAssignmentRepository) FindUnassignedDomains(ctx context.Context, jobID string, limit int) ([]string, error) {
	var domains []string
	// Get distinct domains from pending URLs that are not actively assigned.
	err := r.db.WithContext(ctx).Raw(`
		SELECT DISTINCT
			CASE
				WHEN INSTR(SUBSTR(normalized, INSTR(normalized, '://') + 3), '/') > 0
				THEN SUBSTR(
					SUBSTR(normalized, INSTR(normalized, '://') + 3),
					1,
					INSTR(SUBSTR(normalized, INSTR(normalized, '://') + 3), '/') - 1
				)
				ELSE SUBSTR(normalized, INSTR(normalized, '://') + 3)
			END AS domain
		FROM urls
		WHERE job_id = ? AND status = 'pending'
		AND domain NOT IN (
			SELECT domain FROM domain_assignments
			WHERE job_id = ? AND released_at IS NULL
		)
		LIMIT ?
	`, jobID, jobID, limit).Scan(&domains).Error
	return domains, err
}

func assignmentToModel(a *entities.DomainAssignment) *DomainAssignmentModel {
	m := &DomainAssignmentModel{
		ID:          a.ID,
		WorkerID:    a.WorkerID,
		JobID:       a.JobID,
		Domain:      a.Domain,
		Concurrency: a.Concurrency,
		ActiveCount: a.ActiveCount,
		AssignedAt:  a.AssignedAt,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
	if !a.ReleasedAt.IsZero() {
		m.ReleasedAt = &a.ReleasedAt
	}
	return m
}

func modelToAssignment(m *DomainAssignmentModel) *entities.DomainAssignment {
	a := &entities.DomainAssignment{
		ID:          m.ID,
		WorkerID:    m.WorkerID,
		JobID:       m.JobID,
		Domain:      m.Domain,
		Concurrency: m.Concurrency,
		ActiveCount: m.ActiveCount,
		AssignedAt:  m.AssignedAt,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.ReleasedAt != nil {
		a.ReleasedAt = *m.ReleasedAt
	}
	return a
}

func modelsToAssignments(models []DomainAssignmentModel) []*entities.DomainAssignment {
	result := make([]*entities.DomainAssignment, len(models))
	for i, m := range models {
		result[i] = modelToAssignment(&m)
	}
	return result
}
