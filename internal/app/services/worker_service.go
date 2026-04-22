package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http/extraction"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
)

// WorkerService runs the crawler worker: receives URLs, fetches, extracts, reports.
type WorkerService struct {
	fetcher         ports.Fetcher
	fallbackFetcher ports.Fetcher
	robots          ports.RobotsChecker
	rateLimiter     ports.RateLimiter
	detector        ports.AntiBotDetector
	msgBroker       ports.MessageBroker
	objectStore     ports.ObjectStore
	extractor       *extraction.Extractor
	contentDir      string
	poolSize        int
	taskTimeout     time.Duration
	logger          *slog.Logger
}

const maxTransportPayloadBytes = 900 * 1024

// NewWorkerService creates a new worker service.
func NewWorkerService(
	f ports.Fetcher,
	fallback ports.Fetcher,
	robots ports.RobotsChecker,
	rl ports.RateLimiter,
	detector ports.AntiBotDetector,
	mb ports.MessageBroker,
	objectStore ports.ObjectStore,
	contentDir string,
	poolSize int,
	taskTimeout time.Duration,
	logger *slog.Logger,
) *WorkerService {
	if logger == nil {
		logger = slog.Default()
	}
	if taskTimeout <= 0 {
		taskTimeout = 90 * time.Second
	}
	return &WorkerService{
		fetcher:         f,
		fallbackFetcher: fallback,
		robots:          robots,
		rateLimiter:     rl,
		detector:        detector,
		msgBroker:       mb,
		objectStore:     objectStore,
		extractor:       extraction.NewExtractor(),
		contentDir:      contentDir,
		poolSize:        poolSize,
		taskTimeout:     taskTimeout,
		logger:          logger,
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
		w.logger.Debug("fetch failed",
			"url", task.URL,
			"domain", domain,
			"duration_ms", fetchDuration.Milliseconds(),
			"err", err,
		)
		if w.fallbackFetcher == nil {
			result.Error = fmt.Sprintf("fetch: %v", err)
			w.rateLimiter.RecordResponse(domain, 0, fetchDuration)
			return result
		}
		resp, fetchDuration, err = w.fetchWith(task, ctx, w.fallbackFetcher)
		if err != nil {
			result.Error = fmt.Sprintf("fetch: %v", err)
			w.rateLimiter.RecordResponse(domain, 0, fetchDuration)
			return result
		}
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
			if w.fallbackFetcher != nil {
				fallbackResp, fallbackDuration, fallbackErr := w.fetchWith(task, ctx, w.fallbackFetcher)
				if fallbackErr == nil {
					resp = fallbackResp
					fetchDuration = fallbackDuration
					body, err = extraction.ReadBody(resp.Body)
					if err == nil {
						detection = w.detector.Analyze(resp, body)
						if !detection.Detected {
							goto extract
						}
					}
				}
			}
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

extract:
	// Extract content.
	profile := valueobj.ExtractionProfile{Level: task.Config.Extraction}
	page := w.extractor.Extract(resp, body, task.URLID, task.JobID, task.URL, task.SeedHost, profile, fetchDuration)
	if w.objectStore != nil && shouldTransferContent(page) {
		if err := w.transferContent(ctx, task, page); err != nil {
			result.Error = fmt.Sprintf("transfer content: %v", err)
			return result
		}
	} else if shouldStageContent(page) {
		if err := w.stageContent(task.URL, page); err != nil {
			result.Error = fmt.Sprintf("stage content: %v", err)
			return result
		}
	}
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

func (w *WorkerService) fetchWith(task CrawlTask, ctx context.Context, fetcher ports.Fetcher) (*ports.FetchResponse, time.Duration, error) {
	start := time.Now()
	resp, err := fetcher.Fetch(ctx, task.URL)
	return resp, time.Since(start), err
}

func shouldStageContent(page *entities.Page) bool {
	if page == nil || page.ContentPath != "" {
		return false
	}
	return len(page.RawContent) > 0 || page.HTMLBody != ""
}

func shouldTransferContent(page *entities.Page) bool {
	if page == nil || page.TransferObject != "" {
		return false
	}
	return len(page.RawContent) > 0 || page.HTMLBody != ""
}

func (w *WorkerService) transferContent(ctx context.Context, task CrawlTask, page *entities.Page) error {
	if w.objectStore == nil {
		return nil
	}

	payload := page.RawContent
	if len(payload) == 0 && page.HTMLBody != "" {
		payload = []byte(page.HTMLBody)
	}
	if len(payload) == 0 {
		return nil
	}

	name := transferObjectName(task, page)
	key, err := w.objectStore.PutBytes(ctx, name, payload)
	if err != nil {
		return err
	}
	page.TransferObject = key
	page.ContentSize = int64(len(payload))
	page.RawContent = nil
	page.HTMLBody = ""
	page.ContentPath = ""
	return nil
}

func (w *WorkerService) stageContent(url string, page *entities.Page) error {
	if strings.TrimSpace(w.contentDir) == "" {
		return nil
	}
	payload := page.RawContent
	if len(payload) == 0 && page.HTMLBody != "" {
		payload = []byte(page.HTMLBody)
	}
	if len(payload) == 0 {
		return nil
	}
	path, err := store.BuildContentPath(w.contentDir, url, page.ContentType)
	if err != nil {
		return err
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(".", absPath)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(absPath, payload, 0o644); err != nil {
		return err
	}
	page.ContentPath = filepath.ToSlash(path)
	page.ContentSize = int64(len(payload))
	page.RawContent = nil
	page.HTMLBody = ""
	return nil
}

func compactResultForTransport(result *entities.CrawlResult) ([]byte, error) {
	if result == nil {
		return json.Marshal(result)
	}

	if result.Page != nil {
		// Links are already transported in DiscoveredURLs; avoid sending them twice.
		result.Page.Links = nil
		// Content is staged to disk before transport.
		if result.Page.ContentPath != "" || result.Page.TransferObject != "" {
			result.Page.HTMLBody = ""
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(data) <= maxTransportPayloadBytes {
		return data, nil
	}

	if result.Page != nil && (result.Page.ContentPath != "" || result.Page.TransferObject != "") && result.Page.TextContent != "" {
		result.Page.TextContent = ""
		data, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	}
	if len(data) <= maxTransportPayloadBytes {
		return data, nil
	}

	if result.Page != nil && len(result.Page.StructuredData) > 0 {
		result.Page.StructuredData = nil
		data, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	}
	if len(data) <= maxTransportPayloadBytes {
		return data, nil
	}

	if result.Page != nil && len(result.Page.MetaTags) > 0 {
		result.Page.MetaTags = nil
		data, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	}
	if len(data) <= maxTransportPayloadBytes {
		return data, nil
	}

	if result.Page != nil && len(result.Page.Headers) > 0 {
		result.Page.Headers = nil
		data, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	}
	return data, err
}

// PrepareResultForTransport compacts a crawl result so it can be safely sent over NATS.
func PrepareResultForTransport(result *entities.CrawlResult) ([]byte, error) {
	return compactResultForTransport(result)
}

func transferObjectName(task CrawlTask, page *entities.Page) string {
	return fmt.Sprintf("%s/%s%s", task.JobID, task.URLID, transportContentExtension(task.URL, page.ContentType))
}

func transportContentExtension(rawURL, contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		switch strings.ToLower(mediaType) {
		case "text/html", "application/xhtml+xml":
			return ".html"
		case "application/pdf":
			return ".pdf"
		case "application/json":
			return ".json"
		case "application/xml", "text/xml":
			return ".xml"
		case "text/plain":
			return ".txt"
		}
	}
	ext := strings.ToLower(filepath.Ext(rawURL))
	if ext == "" || len(ext) > 10 {
		return ".bin"
	}
	return ext
}

func isHTMLContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = filepath.Clean(strings.ToLower(mediaType))
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
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

				taskCtx, cancel := context.WithTimeout(ctx, w.taskTimeout)
				defer cancel()

				result := w.ProcessTask(taskCtx, task)
				if errors.Is(taskCtx.Err(), context.Canceled) {
					w.logger.Info("skip publishing canceled task result",
						"job_id", task.JobID,
						"url_id", task.URLID,
						"url", task.URL,
					)
					return
				}

				// Report result back to core.
				resultData, err := compactResultForTransport(result)
				if err != nil {
					w.logger.Error("marshal crawl result",
						"job_id", task.JobID,
						"url_id", task.URLID,
						"url", task.URL,
						"err", err,
					)
					return
				}
				w.logger.Debug("crawl result payload",
					"job_id", task.JobID,
					"url_id", task.URLID,
					"url", task.URL,
					"bytes", len(resultData),
				)
				if err := w.msgBroker.Publish(ctx, broker.CrawlResultSubject(task.JobID), resultData); err != nil {
					w.logger.Error("publish crawl result",
						"job_id", task.JobID,
						"url_id", task.URLID,
						"url", task.URL,
						"err", err,
					)
				}
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
