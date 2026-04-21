package entities

import (
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
)

// WorkerStatus represents the lifecycle state of a worker.
type WorkerStatus string

const (
	WorkerStatusOnline  WorkerStatus = "online"
	WorkerStatusOffline WorkerStatus = "offline"
)

// Worker represents a crawler worker instance with persistent identity.
type Worker struct {
	ID            string       `json:"id"`
	Hostname      string       `json:"hostname"`
	Status        WorkerStatus `json:"status"`
	PoolSize      int          `json:"pool_size"`
	LastHeartbeat time.Time    `json:"last_heartbeat"`
	StartedAt     time.Time    `json:"started_at"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// NewWorker creates a new worker with a generated ID.
func NewWorker(hostname string, poolSize int) *Worker {
	now := time.Now().UTC()
	return &Worker{
		ID:            uid.NewID(),
		Hostname:      hostname,
		Status:        WorkerStatusOnline,
		PoolSize:      poolSize,
		LastHeartbeat: now,
		StartedAt:     now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// RecoverWorker restores a worker from a persisted ID.
func RecoverWorker(id, hostname string, poolSize int) *Worker {
	now := time.Now().UTC()
	return &Worker{
		ID:            id,
		Hostname:      hostname,
		Status:        WorkerStatusOnline,
		PoolSize:      poolSize,
		LastHeartbeat: now,
		StartedAt:     now,
		UpdatedAt:     now,
	}
}

// Heartbeat refreshes the worker's liveness timestamp.
func (w *Worker) Heartbeat() {
	now := time.Now().UTC()
	w.LastHeartbeat = now
	w.UpdatedAt = now
}

// IsAlive returns true if the worker's last heartbeat is within the given TTL.
func (w *Worker) IsAlive(ttl time.Duration) bool {
	return time.Since(w.LastHeartbeat) < ttl
}

// MarkOffline transitions the worker to offline status.
func (w *Worker) MarkOffline() {
	w.Status = WorkerStatusOffline
	w.UpdatedAt = time.Now().UTC()
}

// DomainAssignment represents a domain reserved by a worker.
type DomainAssignment struct {
	ID          string    `json:"id"`
	WorkerID    string    `json:"worker_id"`
	JobID       string    `json:"job_id"`
	Domain      string    `json:"domain"`
	Concurrency int       `json:"concurrency"` // max parallel fetches for this domain
	ActiveCount int       `json:"active_count"` // current in-flight requests
	AssignedAt  time.Time `json:"assigned_at"`
	ReleasedAt  time.Time `json:"released_at,omitzero"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewDomainAssignment creates a new domain assignment.
func NewDomainAssignment(workerID, jobID, domain string, concurrency int) *DomainAssignment {
	now := time.Now().UTC()
	return &DomainAssignment{
		ID:          uid.NewID(),
		WorkerID:    workerID,
		JobID:       jobID,
		Domain:      domain,
		Concurrency: concurrency,
		AssignedAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// Release marks the domain assignment as released.
func (d *DomainAssignment) Release() {
	now := time.Now().UTC()
	d.ReleasedAt = now
	d.UpdatedAt = now
}

// IsActive returns true if the assignment is not yet released.
func (d *DomainAssignment) IsActive() bool {
	return d.ReleasedAt.IsZero()
}
