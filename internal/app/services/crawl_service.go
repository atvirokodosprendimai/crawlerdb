package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/events"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// CrawlService handles URL dispatch and result processing.
type CrawlService struct {
	jobRepo    ports.JobRepository
	urlRepo    ports.URLRepository
	pageRepo   ports.PageRepository
	broker     ports.MessageBroker
	normalizer *services.URLNormalizer
}

// NewCrawlService creates a new CrawlService.
func NewCrawlService(
	jobRepo ports.JobRepository,
	urlRepo ports.URLRepository,
	pageRepo ports.PageRepository,
	broker ports.MessageBroker,
) *CrawlService {
	return &CrawlService{
		jobRepo:    jobRepo,
		urlRepo:    urlRepo,
		pageRepo:   pageRepo,
		broker:     broker,
		normalizer: services.NewURLNormalizer(),
	}
}

// CrawlTask is the message dispatched to crawler workers.
type CrawlTask struct {
	JobID   string             `json:"job_id"`
	URLID   string             `json:"url_id"`
	URL     string             `json:"url"`
	Depth   int                `json:"depth"`
	Config  valueobj.CrawlConfig `json:"config"`
	SeedHost string            `json:"seed_host"`
}

// EnqueueSeedURL adds the initial seed URL to the frontier.
func (s *CrawlService) EnqueueSeedURL(ctx context.Context, job *entities.Job) error {
	norm, err := s.normalizer.Normalize(job.SeedURL, "")
	if err != nil {
		return fmt.Errorf("normalize seed URL: %w", err)
	}

	crawlURL := entities.NewCrawlURL(job.ID, job.SeedURL, norm.Normalized, norm.Hash, 0, "")
	return s.urlRepo.Enqueue(ctx, crawlURL)
}

// DispatchURLs claims pending URLs and publishes them to NATS for workers.
func (s *CrawlService) DispatchURLs(ctx context.Context, jobID string, cfg valueobj.CrawlConfig, limit int) (int, error) {
	claimed, err := s.urlRepo.Claim(ctx, jobID, limit)
	if err != nil {
		return 0, fmt.Errorf("claim URLs: %w", err)
	}

	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return 0, fmt.Errorf("job %s not found", jobID)
	}

	seedHost := extractHost(job.SeedURL)

	for _, u := range claimed {
		task := CrawlTask{
			JobID:    jobID,
			URLID:    u.ID,
			URL:      u.Normalized,
			Depth:    u.Depth,
			Config:   cfg,
			SeedHost: seedHost,
		}
		data, _ := json.Marshal(task)
		if err := s.broker.Publish(ctx, broker.CrawlDispatchSubject(jobID), data); err != nil {
			return 0, fmt.Errorf("publish task: %w", err)
		}
	}

	return len(claimed), nil
}

// ProcessResult handles a crawl result from a worker.
func (s *CrawlService) ProcessResult(ctx context.Context, result *entities.CrawlResult) error {
	// Update URL status.
	if result.Success {
		if err := result.URL.MarkDone(); err != nil {
			return fmt.Errorf("mark URL done: %w", err)
		}
	} else {
		if err := result.URL.MarkError(); err != nil {
			return fmt.Errorf("mark URL error: %w", err)
		}
	}
	if err := s.urlRepo.Complete(ctx, result.URL); err != nil {
		return fmt.Errorf("update URL: %w", err)
	}

	// Store page if successful.
	if result.Success && result.Page != nil {
		if err := s.pageRepo.Store(ctx, result.Page); err != nil {
			return fmt.Errorf("store page: %w", err)
		}
	}

	// Enqueue discovered URLs.
	if len(result.DiscoveredURLs) > 0 {
		job, err := s.jobRepo.FindByID(ctx, result.URL.JobID)
		if err != nil {
			return fmt.Errorf("find job: %w", err)
		}
		if job == nil {
			return nil
		}

		newURLs := s.filterURLs(result.DiscoveredURLs, job, result.URL.Depth+1)
		if len(newURLs) > 0 {
			if err := s.urlRepo.EnqueueBatch(ctx, newURLs); err != nil {
				return fmt.Errorf("enqueue discovered URLs: %w", err)
			}

			// Publish discovery event.
			rawURLs := make([]string, len(result.DiscoveredURLs))
			for i, u := range result.DiscoveredURLs {
				rawURLs[i] = u.Normalized
			}
			evt := events.URLDiscovered{
				Event:     events.NewEvent("url.discovered"),
				JobID:     result.URL.JobID,
				SourceURL: result.URL.Normalized,
				URLs:      rawURLs,
			}
			data, _ := json.Marshal(evt)
			_ = s.broker.Publish(ctx, broker.SubjectURLDiscovered, data)
		}
	}

	return nil
}

// CheckCompletion checks if a job has no more pending/crawling URLs.
func (s *CrawlService) CheckCompletion(ctx context.Context, jobID string) (bool, error) {
	counts, err := s.urlRepo.CountByStatus(ctx, jobID)
	if err != nil {
		return false, err
	}
	pending := counts[entities.URLStatusPending]
	crawling := counts[entities.URLStatusCrawling]
	return pending == 0 && crawling == 0, nil
}

// filterURLs applies scope and depth rules to discovered links.
func (s *CrawlService) filterURLs(links []entities.DiscoveredLink, job *entities.Job, depth int) []*entities.CrawlURL {
	if depth > job.Config.MaxDepth {
		return nil
	}

	seedHost := extractHost(job.SeedURL)
	var urls []*entities.CrawlURL

	for _, link := range links {
		include := false

		switch job.Config.Scope {
		case valueobj.ScopeSameDomain:
			include = s.normalizer.IsInternal(link.Normalized, seedHost)
		case valueobj.ScopeIncludeSubdomain:
			include = s.normalizer.IsSameOrSubdomain(link.Normalized, seedHost)
		case valueobj.ScopeFollowExternals:
			if s.normalizer.IsSameOrSubdomain(link.Normalized, seedHost) {
				include = true
			} else if link.IsExternal && depth <= job.Config.ExternalDepth {
				include = true
			}
		}

		if include {
			u := entities.NewCrawlURL(job.ID, link.RawURL, link.Normalized, link.URLHash, depth, "")
			urls = append(urls, u)
		}
	}

	return urls
}

func extractHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
