package ports

import (
	"context"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
)

// WorkerRepository manages worker persistence.
type WorkerRepository interface {
	Register(ctx context.Context, worker *entities.Worker) error
	FindByID(ctx context.Context, id string) (*entities.Worker, error)
	UpdateHeartbeat(ctx context.Context, workerID string, t time.Time) error
	ListOnline(ctx context.Context) ([]*entities.Worker, error)
	FindStale(ctx context.Context, ttl time.Duration) ([]*entities.Worker, error)
	MarkOffline(ctx context.Context, workerID string) error
}

// DomainAssignmentRepository manages domain-to-worker assignments.
type DomainAssignmentRepository interface {
	Assign(ctx context.Context, assignment *entities.DomainAssignment) error
	FindByWorker(ctx context.Context, workerID string) ([]*entities.DomainAssignment, error)
	FindByDomain(ctx context.Context, jobID, domain string) (*entities.DomainAssignment, error)
	Release(ctx context.Context, assignmentID string) error
	ReleaseByWorker(ctx context.Context, workerID string) error
	ListActive(ctx context.Context, jobID string) ([]*entities.DomainAssignment, error)
	FindUnassignedDomains(ctx context.Context, jobID string, limit int) ([]string, error)
}
