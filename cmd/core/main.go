package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/export"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load config.
	cfg := config.LoadDefault()
	if cfgFile := os.Getenv("CRAWLERDB_CONFIG"); cfgFile != "" {
		if loaded, err := config.LoadFromFile(cfgFile); err == nil {
			cfg = loaded
		} else {
			logger.Warn("failed to load config, using defaults", "err", err)
		}
	}

	// Open database.
	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	if err := store.Migrate(db); err != nil {
		logger.Error("migrate database", "err", err)
		os.Exit(1)
	}

	// Connect to NATS.
	nc, err := nats.Connect(cfg.NATS.URL,
		nats.MaxReconnects(cfg.NATS.MaxReconnects),
		nats.ReconnectWait(cfg.NATS.ReconnectWait.Duration),
		nats.Name("crawlerdb-core"),
	)
	if err != nil {
		logger.Error("connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	mb := broker.NewFromConn(nc)
	mb.SetTimeout(cfg.NATS.RequestTimeout.Duration)
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

	// Create repositories.
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db, store.WithContentDir(cfg.Crawler.ContentDir))
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)

	// Create services.
	jobSvc := services.NewJobService(jobRepo, urlRepo, mb)
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, objectStore, mb)
	exportSvc := services.NewExportService(
		export.NewJSONExporter(pageRepo),
		export.NewCSVExporter(pageRepo, urlRepo),
		export.NewSitemapExporter(urlRepo),
	)
	heartbeats := newWorkerHeartbeatTracker()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Handle job.create requests.
	_, _ = nc.Subscribe("job.create", func(msg *nats.Msg) {
		var req struct {
			SeedURL string               `json:"seed_url"`
			Config  valueobj.CrawlConfig `json:"config"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		job, err := jobSvc.CreateJob(ctx, req.SeedURL, req.Config)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		// Start job and enqueue seed.
		_ = jobSvc.StartJob(ctx, job.ID)
		_ = crawlSvc.EnqueueSeedURL(ctx, job)

		reply, _ := json.Marshal(map[string]string{"job_id": job.ID})
		_ = msg.Respond(reply)

		logger.Info("job created", "id", job.ID, "seed", req.SeedURL)
	})

	_, _ = nc.Subscribe(broker.SubjectWorkerHeartbeat, func(msg *nats.Msg) {
		var hb struct {
			WorkerID  string    `json:"worker_id"`
			Hostname  string    `json:"hostname"`
			Status    string    `json:"status"`
			Timestamp time.Time `json:"timestamp"`
		}
		if err := json.Unmarshal(msg.Data, &hb); err != nil {
			logger.Warn("unmarshal worker heartbeat", "err", err)
			return
		}
		if strings.TrimSpace(hb.WorkerID) == "" {
			return
		}
		ts := hb.Timestamp.UTC()
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		heartbeats.Record(hb.WorkerID, ts)
		if err := retrySQLiteBusy(ctx, 2, 100*time.Millisecond, func() error {
			return workerRepo.UpdateHeartbeat(ctx, hb.WorkerID, ts)
		}); err != nil {
			logger.Debug("update worker heartbeat from broker",
				"worker_id", hb.WorkerID,
				"hostname", hb.Hostname,
				"err", err,
			)
		}
	})

	_, _ = nc.Subscribe("job.retry", func(msg *nats.Msg) {
		var req struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		job, err := jobSvc.RetryJob(ctx, req.JobID)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		_ = jobSvc.StartJob(ctx, job.ID)
		_ = crawlSvc.EnqueueSeedURL(ctx, job)

		reply, _ := json.Marshal(map[string]string{
			"job_id":        job.ID,
			"source_job_id": req.JobID,
		})
		_ = msg.Respond(reply)

		logger.Info("job retried", "source_id", req.JobID, "new_id", job.ID, "seed", job.SeedURL)
	})

	_, _ = nc.Subscribe("job.revisit", func(msg *nats.Msg) {
		var req struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		requeued, err := jobSvc.RevisitJob(ctx, req.JobID)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		reply, _ := json.Marshal(map[string]any{
			"job_id":         req.JobID,
			"requeued_count": requeued,
		})
		_ = msg.Respond(reply)

		logger.Info("job revisit requested", "job_id", req.JobID, "requeued", requeued)
	})

	// Handle job status/stop/pause/resume requests.
	for _, cmd := range []string{"job.status", "job.stop", "job.pause", "job.resume"} {
		subject := cmd
		_, _ = nc.Subscribe(subject, func(msg *nats.Msg) {
			var req struct {
				JobID string `json:"job_id"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				reply, _ := json.Marshal(map[string]string{"error": err.Error()})
				_ = msg.Respond(reply)
				return
			}

			var opErr error
			switch subject {
			case "job.stop":
				opErr = jobSvc.StopJob(ctx, req.JobID)
			case "job.pause":
				opErr = jobSvc.PauseJob(ctx, req.JobID)
			case "job.resume":
				opErr = jobSvc.ResumeJob(ctx, req.JobID)
			}
			if opErr != nil {
				reply, _ := json.Marshal(map[string]string{"error": opErr.Error()})
				_ = msg.Respond(reply)
				return
			}

			job, err := jobSvc.GetJob(ctx, req.JobID)
			if err != nil || job == nil {
				reply, _ := json.Marshal(map[string]string{"error": "job not found"})
				_ = msg.Respond(reply)
				return
			}
			reply, _ := json.Marshal(job)
			_ = msg.Respond(reply)
		})
	}

	// Handle job.list requests.
	_, _ = nc.Subscribe("job.list", func(msg *nats.Msg) {
		jobs, err := jobSvc.ListJobs(ctx, 100, 0)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}
		reply, _ := json.Marshal(jobs)
		_ = msg.Respond(reply)
	})

	// Handle export requests.
	_, _ = nc.Subscribe("job.export", func(msg *nats.Msg) {
		var req struct {
			JobID  string `json:"job_id"`
			Format string `json:"format"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		f, err := os.Create(req.Output)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("create output file: %v", err)})
			_ = msg.Respond(reply)
			return
		}
		defer f.Close()

		err = exportSvc.Export(ctx, ports.ExportFormat(req.Format), ports.ExportFilter{JobID: req.JobID}, f)
		if err != nil {
			reply, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = msg.Respond(reply)
			return
		}

		reply, _ := json.Marshal(map[string]string{"status": "exported", "path": req.Output})
		_ = msg.Respond(reply)
		logger.Info("data exported", "job", req.JobID, "format", req.Format, "path", req.Output)
	})

	// Handle crawl results from workers.
	_, _ = nc.Subscribe("crawl.result.>", func(msg *nats.Msg) {
		var result entities.CrawlResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			logger.Error("unmarshal crawl result", "err", err)
			return
		}

		if err := retrySQLiteBusy(ctx, 8, 250*time.Millisecond, func() error {
			return crawlSvc.ProcessResult(ctx, &result)
		}); err != nil {
			logger.Error("process result", "err", err, "url", result.URL.Normalized)
			return
		}

		// Dispatch more URLs for this job.
		var job *entities.Job
		_ = retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
			var err error
			job, err = jobSvc.GetJob(ctx, result.URL.JobID)
			return err
		})
		if job != nil && job.Status == entities.JobStatusRunning {
			// Check completion.
			var done bool
			_ = retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
				var err error
				done, err = crawlSvc.CheckCompletion(ctx, job.ID)
				return err
			})
			if done {
				_ = retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
					return jobSvc.CompleteJob(ctx, job.ID)
				})
				logger.Info("job completed", "id", job.ID)
			}
		}
	})

	logger.Info("core started", "nats", cfg.NATS.URL, "db", cfg.Database.Path)

	// Run dispatch loop for active jobs. CrawlService enforces per-domain pacing.
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				jobs, _ := jobSvc.ListJobs(ctx, 100, 0)
				for _, job := range jobs {
					if job.Status == entities.JobStatusRunning {
						exhausted := int64(0)
						err := retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
							var err error
							exhausted, err = urlRepo.FailPendingOverRetryLimit(ctx, job.ID, cfg.Crawler.MaxRetries)
							return err
						})
						if err != nil {
							logger.Error("fail pending URLs over retry limit",
								"job_id", job.ID,
								"max_retries", cfg.Crawler.MaxRetries,
								"err", err,
							)
							continue
						}
						if exhausted > 0 {
							logger.Warn("marked pending URLs as error after retry exhaustion",
								"job_id", job.ID,
								"count", exhausted,
								"max_retries", cfg.Crawler.MaxRetries,
							)
						}

						dispatched := 0
						err = retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
							var err error
							dispatched, err = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, cfg.Crawler.PoolSize)
							return err
						})
						if err != nil {
							logger.Error("dispatch urls",
								"job_id", job.ID,
								"err", err,
							)
							continue
						}
						if dispatched > 0 {
							logger.Debug("dispatched urls", "job_id", job.ID, "count", dispatched)
						}
					}
				}
			}
		}
	}()

	// Stale worker reaper — checks every 5s, releases domains for workers dead >15s.
	go func() {
		ttl := cfg.Crawler.HeartbeatTTL.Duration
		if ttl == 0 {
			ttl = 15 * time.Second
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				online, err := workerRepo.ListOnline(ctx)
				if err != nil {
					logger.Error("list online workers", "err", err)
					continue
				}
				now := time.Now().UTC()
				for _, w := range online {
					lastHeartbeat := w.LastHeartbeat.UTC()
					if seen, ok := heartbeats.LastSeen(w.ID); ok && seen.After(lastHeartbeat) {
						lastHeartbeat = seen
					}
					if now.Sub(lastHeartbeat) < ttl {
						continue
					}
					logger.Warn("reaping stale worker",
						"worker_id", w.ID,
						"hostname", w.Hostname,
						"last_heartbeat", lastHeartbeat,
					)
					assignments, err := domainRepo.FindByWorker(ctx, w.ID)
					if err != nil {
						logger.Error("find domains for stale worker", "worker_id", w.ID, "err", err)
					}
					for _, assignment := range assignments {
						requeued, err := urlRepo.RequeueCrawlingByDomain(ctx, assignment.JobID, assignment.Domain)
						if err != nil {
							logger.Error("requeue crawling URLs for stale worker",
								"worker_id", w.ID,
								"job_id", assignment.JobID,
								"domain", assignment.Domain,
								"err", err,
							)
							continue
						}
						if requeued > 0 {
							logger.Warn("requeued crawling URLs for stale worker",
								"worker_id", w.ID,
								"job_id", assignment.JobID,
								"domain", assignment.Domain,
								"count", requeued,
							)
						}
					}
					// Release all domain assignments.
					if err := domainRepo.ReleaseByWorker(ctx, w.ID); err != nil {
						logger.Error("release domains for stale worker", "worker_id", w.ID, "err", err)
					}
					// Mark worker offline.
					if err := workerRepo.MarkOffline(ctx, w.ID); err != nil {
						logger.Error("mark worker offline", "worker_id", w.ID, "err", err)
					}
				}
			}
		}
	}()

	go func() {
		crawlTimeout := cfg.Crawler.CrawlStuckTimeout.Duration
		if crawlTimeout <= 0 {
			crawlTimeout = 10 * time.Minute
		}
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var requeued int64
				var failed int64
				err := retrySQLiteBusy(ctx, 4, 200*time.Millisecond, func() error {
					var err error
					requeued, failed, err = urlRepo.RequeueTimedOutCrawlingWithLimit(ctx, time.Now().Add(-crawlTimeout), cfg.Crawler.MaxRetries)
					return err
				})
				if err != nil {
					logger.Error("requeue timed out crawling URLs", "err", err)
					continue
				}
				if requeued > 0 {
					logger.Warn("requeued timed out crawling URLs", "count", requeued, "timeout", crawlTimeout)
				}
				if failed > 0 {
					logger.Warn("marked timed out crawling URLs as error after retry exhaustion",
						"count", failed,
						"timeout", crawlTimeout,
						"max_retries", cfg.Crawler.MaxRetries,
					)
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				requeued, err := urlRepo.RequeueDueRevisits(ctx, time.Now())
				if err != nil {
					logger.Error("requeue due revisits", "err", err)
					continue
				}
				if requeued > 0 {
					logger.Info("requeued due revisits", "count", requeued)
				}
			}
		}
	}()

	<-ctx.Done()
	logger.Info("core shutting down")
}

func retrySQLiteBusy(ctx context.Context, attempts int, baseDelay time.Duration, fn func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = 100 * time.Millisecond
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = fn()
		if err == nil || !isSQLiteBusy(err) || attempt == attempts {
			return err
		}

		delay := time.Duration(attempt) * baseDelay
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sql logic error: database is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

type workerHeartbeatTracker struct {
	mu       sync.RWMutex
	lastSeen map[string]time.Time
}

func newWorkerHeartbeatTracker() *workerHeartbeatTracker {
	return &workerHeartbeatTracker{
		lastSeen: make(map[string]time.Time),
	}
}

func (t *workerHeartbeatTracker) Record(workerID string, ts time.Time) {
	if workerID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if current, ok := t.lastSeen[workerID]; ok && current.After(ts) {
		return
	}
	t.lastSeen[workerID] = ts
}

func (t *workerHeartbeatTracker) LastSeen(workerID string) (time.Time, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ts, ok := t.lastSeen[workerID]
	return ts, ok
}
