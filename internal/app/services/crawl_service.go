package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/events"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// CrawlService handles URL dispatch and result processing.
type CrawlService struct {
	jobRepo     ports.JobRepository
	urlRepo     ports.URLRepository
	pageRepo    ports.PageRepository
	objectStore ports.ObjectStore
	broker      ports.MessageBroker
	normalizer  *services.URLNormalizer
	mu          sync.Mutex
	lastSent    map[string]time.Time
}

// NewCrawlService creates a new CrawlService.
func NewCrawlService(
	jobRepo ports.JobRepository,
	urlRepo ports.URLRepository,
	pageRepo ports.PageRepository,
	objectStore ports.ObjectStore,
	broker ports.MessageBroker,
) *CrawlService {
	return &CrawlService{
		jobRepo:     jobRepo,
		urlRepo:     urlRepo,
		pageRepo:    pageRepo,
		objectStore: objectStore,
		broker:      broker,
		normalizer:  services.NewURLNormalizer(),
		lastSent:    make(map[string]time.Time),
	}
}

// CrawlTask is the message dispatched to crawler workers.
type CrawlTask struct {
	JobID    string               `json:"job_id"`
	URLID    string               `json:"url_id"`
	URL      string               `json:"url"`
	Depth    int                  `json:"depth"`
	Config   valueobj.CrawlConfig `json:"config"`
	SeedHost string               `json:"seed_host"`
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
	if limit <= 0 {
		return 0, nil
	}

	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return 0, fmt.Errorf("job %s not found", jobID)
	}

	counts, err := s.urlRepo.CountByStatus(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("count URL statuses: %w", err)
	}
	available := limit - counts[entities.URLStatusCrawling]
	if available <= 0 {
		return 0, nil
	}

	seedHost := extractHost(job.SeedURL)
	selectedIDs, err := s.selectDispatchIDs(ctx, jobID, cfg, available)
	if err != nil {
		return 0, fmt.Errorf("select dispatch URLs: %w", err)
	}
	if len(selectedIDs) == 0 {
		return 0, nil
	}

	claimed, err := s.urlRepo.ClaimByIDs(ctx, jobID, selectedIDs)
	if err != nil {
		return 0, fmt.Errorf("claim URLs: %w", err)
	}

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
		s.markDispatched(extractHost(u.Normalized))
	}

	return len(claimed), nil
}

func (s *CrawlService) selectDispatchIDs(ctx context.Context, jobID string, cfg valueobj.CrawlConfig, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	searchLimit := max(limit*20, 100)
	pending, err := s.urlRepo.FindPending(ctx, jobID, searchLimit)
	if err != nil {
		return nil, err
	}

	interval := cfg.RateLimit.Duration
	now := time.Now()
	selected := make([]string, 0, limit)
	reservedDomains := make(map[string]struct{}, limit)
	inFlightByDomain, err := s.urlRepo.CountCrawlingByDomain(ctx, jobID)
	if err != nil {
		return nil, err
	}
	maxConcurrency := cfg.MaxConcurrency

	for _, u := range pending {
		if len(selected) >= limit {
			break
		}
		domain := extractHost(u.Normalized)
		if domain == "" {
			continue
		}
		if maxConcurrency > 0 && inFlightByDomain[domain] >= maxConcurrency {
			continue
		}
		if interval > 0 {
			if _, exists := reservedDomains[domain]; exists {
				continue
			}
		}
		if interval > 0 && !s.canDispatch(domain, now, interval) {
			continue
		}
		selected = append(selected, u.ID)
		if interval > 0 {
			reservedDomains[domain] = struct{}{}
		}
		inFlightByDomain[domain]++
	}

	return selected, nil
}

func (s *CrawlService) canDispatch(domain string, now time.Time, interval time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastSent[domain]
	if !ok {
		return true
	}
	return now.Sub(last) >= interval
}

func (s *CrawlService) markDispatched(domain string) {
	if domain == "" {
		return
	}
	s.mu.Lock()
	s.lastSent[domain] = time.Now()
	s.mu.Unlock()
}

// ProcessResult handles a crawl result from a worker.
func (s *CrawlService) ProcessResult(ctx context.Context, result *entities.CrawlResult) error {
	job, err := s.jobRepo.FindByID(ctx, result.URL.JobID)
	if err != nil {
		return fmt.Errorf("find job: %w", err)
	}

	// Update URL status.
	if result.Success {
		result.URL.LastError = ""
		if job != nil && job.Config.RevisitTTL.Duration > 0 {
			result.URL.ScheduleRevisit(job.Config.RevisitTTL.Duration)
		}
		if err := result.URL.MarkDone(); err != nil {
			return fmt.Errorf("mark URL done: %w", err)
		}
	} else {
		result.URL.LastError = result.Error
		if shouldMarkBlocked(result) {
			if err := result.URL.MarkBlocked(); err != nil {
				return fmt.Errorf("mark URL blocked: %w", err)
			}
		} else {
			if err := result.URL.MarkError(); err != nil {
				return fmt.Errorf("mark URL error: %w", err)
			}
		}
	}
	if err := s.urlRepo.Complete(ctx, result.URL); err != nil {
		return fmt.Errorf("update URL: %w", err)
	}

	// Store page if successful.
	if result.Success && result.Page != nil {
		if err := s.hydrateTransferredPage(ctx, result.Page); err != nil {
			return fmt.Errorf("hydrate transferred page: %w", err)
		}
		if len(result.Page.Links) == 0 && len(result.DiscoveredURLs) > 0 {
			result.Page.Links = append([]entities.DiscoveredLink(nil), result.DiscoveredURLs...)
		}
		if err := s.pageRepo.Store(ctx, result.Page); err != nil {
			return fmt.Errorf("store page: %w", err)
		}
		if err := s.releaseTransferredPage(ctx, result.Page); err != nil {
			return fmt.Errorf("release transferred page: %w", err)
		}
	}

	// Enqueue discovered URLs.
	if len(result.DiscoveredURLs) > 0 {
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

func (s *CrawlService) hydrateTransferredPage(ctx context.Context, page *entities.Page) error {
	if page == nil || page.TransferObject == "" {
		return nil
	}
	if s.objectStore == nil {
		return fmt.Errorf("object store not configured for transfer object %q", page.TransferObject)
	}

	data, err := s.objectStore.GetBytes(ctx, page.TransferObject)
	if err != nil {
		return fmt.Errorf("get transfer object %q: %w", page.TransferObject, err)
	}
	page.RawContent = data
	page.ContentPath = ""
	page.ContentSize = int64(len(data))
	return nil
}

func (s *CrawlService) releaseTransferredPage(ctx context.Context, page *entities.Page) error {
	if page == nil || page.TransferObject == "" {
		return nil
	}
	if s.objectStore == nil {
		return fmt.Errorf("object store not configured for transfer object %q", page.TransferObject)
	}
	if err := s.objectStore.Delete(ctx, page.TransferObject); err != nil {
		return fmt.Errorf("delete transfer object %q: %w", page.TransferObject, err)
	}
	page.TransferObject = ""
	return nil
}

func shouldMarkBlocked(result *entities.CrawlResult) bool {
	if result == nil {
		return false
	}
	if result.AntiBotEvent != nil {
		return true
	}
	return strings.Contains(strings.ToLower(result.Error), "blocked")
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
