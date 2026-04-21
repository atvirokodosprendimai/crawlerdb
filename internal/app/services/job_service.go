package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/events"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// JobService manages crawl job lifecycle.
type JobService struct {
	jobRepo ports.JobRepository
	urlRepo ports.URLRepository
	broker  ports.MessageBroker
}

// NewJobService creates a new JobService.
func NewJobService(jobRepo ports.JobRepository, urlRepo ports.URLRepository, broker ports.MessageBroker) *JobService {
	return &JobService{
		jobRepo: jobRepo,
		urlRepo: urlRepo,
		broker:  broker,
	}
}

// CreateJob creates a new crawl job and enqueues the seed URL.
func (s *JobService) CreateJob(ctx context.Context, seedURL string, cfg valueobj.CrawlConfig) (*entities.Job, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	job := entities.NewJob(seedURL, cfg)
	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}

	// Publish job created event.
	evt := events.JobCreated{
		Event:   events.NewEvent("job.created"),
		JobID:   job.ID,
		SeedURL: seedURL,
	}
	data, _ := json.Marshal(evt)
	_ = s.broker.Publish(ctx, broker.SubjectJobCreated, data)

	return job, nil
}

// StartJob transitions job to running and enqueues the seed URL.
func (s *JobService) StartJob(ctx context.Context, jobID string) error {
	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return fmt.Errorf("job %s not found", jobID)
	}

	if err := job.Start(); err != nil {
		return fmt.Errorf("start job: %w", err)
	}
	if err := s.jobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	// Publish update event.
	s.publishJobUpdate(ctx, job)
	return nil
}

// PauseJob pauses a running job.
func (s *JobService) PauseJob(ctx context.Context, jobID string) error {
	return s.transitionJob(ctx, jobID, func(j *entities.Job) error { return j.Pause() })
}

// ResumeJob resumes a paused job.
func (s *JobService) ResumeJob(ctx context.Context, jobID string) error {
	return s.transitionJob(ctx, jobID, func(j *entities.Job) error { return j.Resume() })
}

// StopJob stops a running or paused job.
func (s *JobService) StopJob(ctx context.Context, jobID string) error {
	return s.transitionJob(ctx, jobID, func(j *entities.Job) error { return j.Stop() })
}

// CompleteJob marks a job as completed.
func (s *JobService) CompleteJob(ctx context.Context, jobID string) error {
	return s.transitionJob(ctx, jobID, func(j *entities.Job) error { return j.Complete() })
}

// GetJob returns a job by ID.
func (s *JobService) GetJob(ctx context.Context, jobID string) (*entities.Job, error) {
	return s.jobRepo.FindByID(ctx, jobID)
}

// ListJobs returns all jobs with pagination.
func (s *JobService) ListJobs(ctx context.Context, limit, offset int) ([]*entities.Job, error) {
	return s.jobRepo.List(ctx, limit, offset)
}

func (s *JobService) transitionJob(ctx context.Context, jobID string, transition func(*entities.Job) error) error {
	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return fmt.Errorf("job %s not found", jobID)
	}

	if err := transition(job); err != nil {
		return err
	}
	if err := s.jobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	s.publishJobUpdate(ctx, job)
	return nil
}

func (s *JobService) publishJobUpdate(ctx context.Context, job *entities.Job) {
	evt := events.JobUpdated{
		Event:  events.NewEvent("job.updated"),
		JobID:  job.ID,
		Status: job.Status,
	}
	data, _ := json.Marshal(evt)
	_ = s.broker.Publish(ctx, broker.SubjectJobUpdated, data)
}
