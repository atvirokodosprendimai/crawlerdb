package entities

import (
	"errors"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
)

// ErrInvalidTransition is returned when a job state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid state transition")

// JobStatus represents the current state of a crawl job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusPaused    JobStatus = "paused"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusStopped   JobStatus = "stopped"
)

// JobStats holds aggregate statistics for a crawl job.
type JobStats struct {
	PagesCrawled int `json:"pages_crawled"`
	PagesErrored int `json:"pages_errored"`
	PagesBlocked int `json:"pages_blocked"`
	URLsFound    int `json:"urls_found"`
}

// Job is the root aggregate for a crawl operation.
type Job struct {
	ID             string               `json:"id"`
	SeedURL        string               `json:"seed_url"`
	Config         valueobj.CrawlConfig `json:"config"`
	Status         JobStatus            `json:"status"`
	Stats          JobStats             `json:"stats"`
	Error          string               `json:"error,omitempty"`
	DeleteMarkedAt time.Time            `json:"delete_marked_at,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	StartedAt      time.Time            `json:"started_at,omitempty"`
	FinishedAt     time.Time            `json:"finished_at,omitempty"`
}

// NewJob creates a new job in pending state.
func NewJob(seedURL string, cfg valueobj.CrawlConfig) *Job {
	now := time.Now().UTC()
	return &Job{
		ID:        uid.NewID(),
		SeedURL:   seedURL,
		Config:    cfg,
		Status:    JobStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// validTransitions defines the state machine.
var validTransitions = map[JobStatus][]JobStatus{
	JobStatusPending: {JobStatusRunning},
	JobStatusRunning: {JobStatusPaused, JobStatusCompleted, JobStatusFailed, JobStatusStopped},
	JobStatusPaused:  {JobStatusRunning, JobStatusStopped},
}

func (j *Job) transitionTo(target JobStatus) error {
	allowed, ok := validTransitions[j.Status]
	if !ok {
		return ErrInvalidTransition
	}
	for _, s := range allowed {
		if s == target {
			j.Status = target
			j.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return ErrInvalidTransition
}

// Start transitions the job to running state.
func (j *Job) Start() error {
	if err := j.transitionTo(JobStatusRunning); err != nil {
		return err
	}
	j.StartedAt = j.UpdatedAt
	return nil
}

// Pause transitions the job to paused state.
func (j *Job) Pause() error {
	return j.transitionTo(JobStatusPaused)
}

// Resume transitions the job back to running from paused.
func (j *Job) Resume() error {
	return j.transitionTo(JobStatusRunning)
}

// Complete transitions the job to completed state.
func (j *Job) Complete() error {
	if err := j.transitionTo(JobStatusCompleted); err != nil {
		return err
	}
	j.FinishedAt = j.UpdatedAt
	return nil
}

// Fail transitions the job to failed state with an error message.
func (j *Job) Fail(reason string) error {
	if err := j.transitionTo(JobStatusFailed); err != nil {
		return err
	}
	j.Error = reason
	j.FinishedAt = j.UpdatedAt
	return nil
}

// Stop transitions the job to stopped state (user-initiated).
func (j *Job) Stop() error {
	if err := j.transitionTo(JobStatusStopped); err != nil {
		return err
	}
	j.FinishedAt = j.UpdatedAt
	return nil
}

// Revisit reactivates a job so already-known URLs can be crawled again.
func (j *Job) Revisit() error {
	now := time.Now().UTC()

	switch j.Status {
	case JobStatusPending:
		j.Status = JobStatusRunning
		if j.StartedAt.IsZero() {
			j.StartedAt = now
		}
	case JobStatusRunning:
		// Keep the job active and only bump the timestamp.
	case JobStatusPaused, JobStatusCompleted, JobStatusFailed, JobStatusStopped:
		j.Status = JobStatusRunning
		if j.StartedAt.IsZero() {
			j.StartedAt = now
		}
	default:
		return ErrInvalidTransition
	}

	j.Error = ""
	j.FinishedAt = time.Time{}
	j.UpdatedAt = now
	return nil
}

// IsTerminal returns true if the job is in a final state.
func (j *Job) IsTerminal() bool {
	switch j.Status {
	case JobStatusCompleted, JobStatusFailed, JobStatusStopped:
		return true
	}
	return false
}
