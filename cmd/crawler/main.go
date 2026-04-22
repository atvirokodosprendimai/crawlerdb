package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/antibot"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http/extraction"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/robots"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/worker"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(*debug)}))

	// Load config.
	cfg := config.LoadDefault()
	if cfgFile := os.Getenv("CRAWLERDB_CONFIG"); cfgFile != "" {
		if loaded, err := config.LoadFromFile(cfgFile); err == nil {
			cfg = loaded
		}
	}

	// Load or create persistent worker identity.
	identity, err := worker.LoadOrCreate(cfg.Crawler.DataDir)
	if err != nil {
		logger.Error("load worker identity", "err", err)
		os.Exit(1)
	}
	logger.Info("worker identity", "id", identity.ID(), "path", identity.Path())

	hostname, _ := os.Hostname()

	// Open database (shared SQLite for worker registry).
	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	if err := store.Migrate(db); err != nil {
		logger.Error("migrate database", "err", err)
		os.Exit(1)
	}

	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)

	// Connect to NATS.
	nc, err := nats.Connect(cfg.NATS.URL,
		nats.MaxReconnects(cfg.NATS.MaxReconnects),
		nats.ReconnectWait(cfg.NATS.ReconnectWait.Duration),
		nats.Name("crawlerdb-crawler-"+identity.ID()[:8]),
	)
	if err != nil {
		logger.Error("connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	mb := broker.NewFromConn(nc)
	objectStore, err := broker.NewObjectStore(nc, jetstream.ObjectStoreConfig{
		Bucket:      cfg.NATS.ObjectStoreBucket,
		Description: "crawlerdb transfer payloads",
		TTL:         cfg.NATS.ObjectStoreTTL.Duration,
		MaxBytes:    cfg.NATS.ObjectStoreMaxBytes,
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		logger.Error("create object store", "bucket", cfg.NATS.ObjectStoreBucket, "err", err)
		os.Exit(1)
	}

	// Register worker.
	w := entities.RecoverWorker(identity.ID(), hostname, cfg.Crawler.PoolSize)
	if err := workerRepo.Register(context.Background(), w); err != nil {
		logger.Error("register worker", "err", err)
		os.Exit(1)
	}

	// Check if we have existing domain assignments (resume after restart).
	existingAssignments, err := domainRepo.FindByWorker(context.Background(), identity.ID())
	if err != nil {
		logger.Error("find existing assignments", "err", err)
		os.Exit(1)
	}
	if len(existingAssignments) > 0 {
		logger.Info("resuming existing assignments",
			"count", len(existingAssignments),
			"domains", assignmentDomains(existingAssignments),
		)
	}

	// Create fetcher and helpers.
	httpFetcher := fetcher.New(
		fetcher.WithUserAgent(cfg.Crawler.UserAgent),
		fetcher.WithTimeout(cfg.Crawler.RequestTimeout.Duration),
	)
	var chromiumFetcher *fetcher.ChromiumFetcher
	if cfg.Crawler.ChromiumURL != "" {
		chromiumFetcher = fetcher.NewChromium(cfg.Crawler.ChromiumURL, cfg.Crawler.UserAgent, cfg.Crawler.RequestTimeout.Duration*2)
	}
	robotsChecker := robots.NewChecker(httpFetcher, cfg.Crawler.UserAgent, cfg.Crawler.RobotsTTL.Duration)
	rateLimiter := fetcher.NewAdaptiveRateLimiter(cfg.Crawler.DefaultRateLimit.Duration)
	detector := antibot.NewDetector()
	ext := extraction.NewExtractor()
	taskTimeout := cfg.Crawler.RequestTimeout.Duration * 3
	if taskTimeout <= 0 {
		taskTimeout = 90 * time.Second
	}

	workerSvc := services.NewWorkerService(httpFetcher, chromiumFetcher, robotsChecker, rateLimiter, detector, mb, objectStore, cfg.Crawler.ContentDir, cfg.Crawler.PoolSize, taskTimeout, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Subscribe to domain-specific dispatch subjects.
	sem := make(chan struct{}, cfg.Crawler.PoolSize)
	var wg sync.WaitGroup
	var assignmentMu sync.RWMutex

	_, _ = nc.QueueSubscribe("crawl.dispatch.>", broker.QueueGroupCrawler, func(msg *nats.Msg) {
		var task services.CrawlTask
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			logger.Error("unmarshal task", "err", err)
			return
		}
		logger.Debug("task received",
			"job_id", task.JobID,
			"url_id", task.URLID,
			"url", task.URL,
			"depth", task.Depth,
			"seed_host", task.SeedHost,
		)

		// Check if this domain is assigned to us.
		taskDomain := extractDomain(task.URL)
		assigned := false
		assignmentMu.RLock()
		for _, a := range existingAssignments {
			if a.Domain == taskDomain && a.IsActive() {
				assigned = true
				break
			}
		}
		assignmentMu.RUnlock()

		// If domain not assigned, try to claim it.
		if !assigned {
			existing, _ := domainRepo.FindByDomain(ctx, task.JobID, taskDomain)
			if existing != nil && existing.WorkerID != identity.ID() {
				// Task was already claimed and marked crawling by core. Never drop it here,
				// or the URL will remain stuck in crawling forever with no result.
				logger.Warn("processing task despite foreign domain assignment",
					"domain", taskDomain,
					"job_id", task.JobID,
					"owner_worker_id", existing.WorkerID,
					"worker_id", identity.ID(),
					"url", task.URL,
				)
			}
			if existing == nil {
				// Claim domain.
				assignment := entities.NewDomainAssignment(
					identity.ID(), task.JobID, taskDomain, cfg.Crawler.DomainConcurrency,
				)
				if err := domainRepo.Assign(ctx, assignment); err != nil {
					logger.Warn("domain claim failed; processing task anyway",
						"domain", taskDomain,
						"job_id", task.JobID,
						"worker_id", identity.ID(),
						"url", task.URL,
						"err", err,
					)
				} else {
					assignmentMu.Lock()
					existingAssignments = append(existingAssignments, assignment)
					assignmentMu.Unlock()
					logger.Info("claimed domain", "domain", taskDomain, "job", task.JobID)
				}
			}
		}

		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
			defer cancel()

			result := workerSvc.ProcessTask(taskCtx, task)
			logger.Debug("task processed",
				"job_id", task.JobID,
				"url_id", task.URLID,
				"url", task.URL,
				"success", result.Success,
				"error", result.Error,
				"discovered_urls", len(result.DiscoveredURLs),
			)

			resultData, err := services.PrepareResultForTransport(result)
			if err != nil {
				logger.Error("marshal crawl result",
					"job_id", task.JobID,
					"url_id", task.URLID,
					"url", task.URL,
					"err", err,
				)
				return
			}
			if err := mb.Publish(ctx, broker.CrawlResultSubject(task.JobID), resultData); err != nil {
				logger.Error("publish crawl result",
					"job_id", task.JobID,
					"subject", broker.CrawlResultSubject(task.JobID),
					"url", task.URL,
					"bytes", len(resultData),
					"err", err,
				)
				return
			}
			logger.Debug("result published",
				"job_id", task.JobID,
				"subject", broker.CrawlResultSubject(task.JobID),
				"url", task.URL,
				"bytes", len(resultData),
			)
		}()
	})

	// Heartbeat loop — every 5s.
	go func() {
		ticker := time.NewTicker(cfg.Crawler.HeartbeatInterval.Duration)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC()
				hb := map[string]any{
					"worker_id": identity.ID(),
					"hostname":  hostname,
					"status":    "alive",
					"domains": func() []string {
						assignmentMu.RLock()
						defer assignmentMu.RUnlock()
						return assignmentDomains(existingAssignments)
					}(),
					"timestamp": now,
				}
				data, _ := json.Marshal(hb)
				if err := nc.Publish(broker.SubjectWorkerHeartbeat, data); err != nil {
					logger.Warn("publish worker heartbeat", "worker_id", identity.ID(), "err", err)
				}
			}
		}
	}()

	logger.Info("crawler started",
		"worker_id", identity.ID(),
		"pool_size", cfg.Crawler.PoolSize,
		"heartbeat", cfg.Crawler.HeartbeatInterval.Duration,
		"ttl", cfg.Crawler.HeartbeatTTL.Duration,
		"domain_concurrency", cfg.Crawler.DomainConcurrency,
	)

	_ = workerSvc // Worker ready via NATS subscriptions.
	_ = ext       // Extractor available.
	<-ctx.Done()

	logger.Info("crawler shutting down", "worker_id", identity.ID())

	// Release domain assignments on graceful shutdown.
	if err := domainRepo.ReleaseByWorker(context.Background(), identity.ID()); err != nil {
		logger.Error("release domains", "err", err)
	}
	_ = workerRepo.MarkOffline(context.Background(), identity.ID())

	wg.Wait()
	logger.Info("crawler stopped")
}

func logLevel(debug bool) slog.Level {
	if debug {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func assignmentDomains(assignments []*entities.DomainAssignment) []string {
	domains := make([]string, 0, len(assignments))
	for _, a := range assignments {
		if a.IsActive() {
			domains = append(domains, a.Domain)
		}
	}
	return domains
}

func extractDomain(rawURL string) string {
	// Quick domain extraction — strip scheme, strip path.
	s := rawURL
	if i := len("https://"); len(s) > i && (s[:i] == "https://" || s[:7] == "http://") {
		if s[:5] == "https" {
			s = s[8:]
		} else {
			s = s[7:]
		}
	}
	for i, c := range s {
		if c == '/' || c == '?' || c == '#' {
			return s[:i]
		}
	}
	return s
}
