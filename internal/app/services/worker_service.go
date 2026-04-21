package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http/extraction"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// WorkerService runs the crawler worker: receives URLs, fetches, extracts, reports.
type WorkerService struct {
	fetcher     ports.Fetcher
	robots      ports.RobotsChecker
	rateLimiter ports.RateLimiter
	detector    ports.AntiBotDetector
	msgBroker   ports.MessageBroker
	extractor   *extraction.Extractor
	poolSize    int
	logger      *slog.Logger
}

// NewWorkerService creates a new worker service.
func NewWorkerService(
	f ports.Fetcher,
	robots ports.RobotsChecker,
	rl ports.RateLimiter,
	detector ports.AntiBotDetector,
	mb ports.MessageBroker,
	poolSize int,
	logger *slog.Logger,
) *WorkerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkerService{
		fetcher:     f,
		robots:      robots,
		rateLimiter: rl,
		detector:    detector,
		msgBroker:   mb,
		extractor:   extraction.NewExtractor(),
		poolSize:    poolSize,
		logger:      logger,
	}
}

// ProcessTask handles a single crawl task.
func (w *WorkerService) ProcessTask(ctx context.Context, task CrawlTask) *entities.CrawlResult {
	w.logger.Debug("process task",
		"job_id", task.JobID,
		"url_id", task.URLID,
		"url", task.URL,
		"depth", task.Depth,
	)

	crawlURL := &entities.CrawlURL{
		ID:         task.URLID,
		JobID:      task.JobID,
		Normalized: task.URL,
		Depth:      task.Depth,
		Status:     entities.URLStatusCrawling,
	}

	result := &entities.CrawlResult{
		URL: crawlURL,
	}

	// Check robots.txt.
	allowed, err := w.robots.IsAllowed(ctx, task.URL, task.Config.UserAgent)
	if err != nil {
		w.logger.Warn("robots check failed", "url", task.URL, "err", err)
	}
	if !allowed {
		w.logger.Debug("robots blocked url", "url", task.URL)
		result.Error = "blocked by robots.txt"
		return result
	}
	w.logger.Debug("robots allowed url", "url", task.URL)

	// Rate limit.
	domain := extractHost(task.URL)
	waitStart := time.Now()
	if err := w.rateLimiter.Wait(ctx, domain); err != nil {
		result.Error = fmt.Sprintf("rate limit wait: %v", err)
		return result
	}
	w.logger.Debug("rate limiter granted",
		"url", task.URL,
		"domain", domain,
		"wait_ms", time.Since(waitStart).Milliseconds(),
	)

	// Fetch.
	start := time.Now()
	w.logger.Debug("fetch start", "url", task.URL, "domain", domain)
	resp, err := w.fetcher.Fetch(ctx, task.URL)
	fetchDuration := time.Since(start)
	if err != nil {
		result.Error = fmt.Sprintf("fetch: %v", err)
		w.rateLimiter.RecordResponse(domain, 0, fetchDuration)
		w.logger.Debug("fetch failed",
			"url", task.URL,
			"domain", domain,
			"duration_ms", fetchDuration.Milliseconds(),
			"err", err,
		)
		return result
	}
	w.logger.Debug("fetch complete",
		"url", task.URL,
		"final_url", resp.URL,
		"status", resp.StatusCode,
		"content_type", resp.ContentType,
		"duration_ms", fetchDuration.Milliseconds(),
	)

	// Record response for adaptive rate limiting.
	w.rateLimiter.RecordResponse(domain, resp.StatusCode, fetchDuration)

	// Read body.
	body, err := extraction.ReadBody(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("read body: %v", err)
		return result
	}
	w.logger.Debug("body read",
		"url", task.URL,
		"bytes", len(body),
	)

	// Anti-bot detection.
	if w.detector != nil {
		detection := w.detector.Analyze(resp, body)
		if detection.Detected {
			w.logger.Warn("anti-bot detected",
				"url", task.URL,
				"event", detection.EventType,
				"provider", detection.Provider,
			)
			result.Error = fmt.Sprintf("anti-bot: %s (%s)", detection.EventType, detection.Provider)
			result.AntiBotEvent = &entities.AntiBotDetection{
				Detected:  detection.Detected,
				EventType: detection.EventType,
				Provider:  detection.Provider,
				Details:   detection.Details,
			}
			return result
		}
	}

	// Extract content.
	profile := valueobj.ExtractionProfile{Level: task.Config.Extraction}
	page := w.extractor.Extract(resp, body, task.URLID, task.JobID, task.URL, task.SeedHost, profile, fetchDuration)
	w.logger.Debug("extraction complete",
		"url", task.URL,
		"title", page.Title,
		"links", len(page.Links),
		"http_status", page.HTTPStatus,
		"content_type", page.ContentType,
	)

	result.Page = page
	result.DiscoveredURLs = page.Links
	result.Success = true
	w.logger.Debug("task success",
		"url", task.URL,
		"discovered_urls", len(result.DiscoveredURLs),
	)

	return result
}

// Run starts the worker pool, subscribing to NATS for crawl tasks.
func (w *WorkerService) Run(ctx context.Context, jobIDs []string) error {
	sem := make(chan struct{}, w.poolSize)
	var wg sync.WaitGroup

	for _, jobID := range jobIDs {
		subject := broker.CrawlDispatchSubject(jobID)
		_, err := w.msgBroker.QueueSubscribe(subject, broker.QueueGroupCrawler, func(subj string, data []byte) error {
			var task CrawlTask
			if err := json.Unmarshal(data, &task); err != nil {
				w.logger.Error("unmarshal task", "err", err)
				return nil
			}

			sem <- struct{}{} // acquire
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }() // release

				result := w.ProcessTask(ctx, task)

				// Report result back to core.
				resultData, _ := json.Marshal(result)
				_ = w.msgBroker.Publish(ctx, broker.CrawlResultSubject(task.JobID), resultData)
			}()

			return nil
		})
		if err != nil {
			return fmt.Errorf("subscribe to %s: %w", subject, err)
		}
	}

	// Wait for context cancellation.
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}
