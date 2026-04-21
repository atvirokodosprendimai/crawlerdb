package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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

	// Create repositories.
	jobRepo := store.NewJobRepository(db)
	urlRepo := store.NewURLRepository(db)
	pageRepo := store.NewPageRepository(db)
	workerRepo := store.NewWorkerRepository(db)
	domainRepo := store.NewDomainAssignmentRepository(db)

	// Create services.
	jobSvc := services.NewJobService(jobRepo, urlRepo, mb)
	crawlSvc := services.NewCrawlService(jobRepo, urlRepo, pageRepo, mb)
	exportSvc := services.NewExportService(
		export.NewJSONExporter(pageRepo),
		export.NewCSVExporter(pageRepo, urlRepo),
		export.NewSitemapExporter(urlRepo),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Handle job.create requests.
	_, _ = nc.Subscribe("job.create", func(msg *nats.Msg) {
		var req struct {
			SeedURL string              `json:"seed_url"`
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

		if err := crawlSvc.ProcessResult(ctx, &result); err != nil {
			logger.Error("process result", "err", err, "url", result.URL.Normalized)
			return
		}

		// Dispatch more URLs for this job.
		job, _ := jobSvc.GetJob(ctx, result.URL.JobID)
		if job != nil && job.Status == entities.JobStatusRunning {
			_, _ = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 10)

			// Check completion.
			done, _ := crawlSvc.CheckCompletion(ctx, job.ID)
			if done {
				_ = jobSvc.CompleteJob(ctx, job.ID)
				logger.Info("job completed", "id", job.ID)
			}
		}
	})

	logger.Info("core started", "nats", cfg.NATS.URL, "db", cfg.Database.Path)

	// Run dispatch loop for active jobs.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				jobs, _ := jobSvc.ListJobs(ctx, 100, 0)
				for _, job := range jobs {
					if job.Status == entities.JobStatusRunning {
						_, _ = crawlSvc.DispatchURLs(ctx, job.ID, job.Config, 10)
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
				stale, err := workerRepo.FindStale(ctx, ttl)
				if err != nil {
					logger.Error("find stale workers", "err", err)
					continue
				}
				for _, w := range stale {
					logger.Warn("reaping stale worker",
						"worker_id", w.ID,
						"hostname", w.Hostname,
						"last_heartbeat", w.LastHeartbeat,
					)
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

	<-ctx.Done()
	logger.Info("core shutting down")
}
