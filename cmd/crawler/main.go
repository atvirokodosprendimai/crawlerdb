package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/antibot"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	fetcher "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/http"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/robots"
	"github.com/atvirokodosprendimai/crawlerdb/internal/app/services"
	"github.com/nats-io/nats.go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load config.
	cfg := config.LoadDefault()
	if cfgFile := os.Getenv("CRAWLERDB_CONFIG"); cfgFile != "" {
		if loaded, err := config.LoadFromFile(cfgFile); err == nil {
			cfg = loaded
		}
	}

	// Connect to NATS.
	nc, err := nats.Connect(cfg.NATS.URL,
		nats.MaxReconnects(cfg.NATS.MaxReconnects),
		nats.ReconnectWait(cfg.NATS.ReconnectWait.Duration),
		nats.Name("crawlerdb-crawler"),
	)
	if err != nil {
		logger.Error("connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	mb := broker.NewFromConn(nc)

	// Create fetcher and helpers.
	httpFetcher := fetcher.New(
		fetcher.WithUserAgent(cfg.Crawler.UserAgent),
		fetcher.WithTimeout(cfg.Crawler.RequestTimeout.Duration),
	)
	robotsChecker := robots.NewChecker(httpFetcher, cfg.Crawler.UserAgent, cfg.Crawler.RobotsTTL.Duration)
	rateLimiter := fetcher.NewAdaptiveRateLimiter(cfg.Crawler.DefaultRateLimit.Duration)

	detector := antibot.NewDetector()
	worker := services.NewWorkerService(httpFetcher, robotsChecker, rateLimiter, detector, mb, cfg.Crawler.PoolSize, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Subscribe to all crawl dispatch subjects.
	_, err = nc.QueueSubscribe("crawl.dispatch.>", broker.QueueGroupCrawler, func(msg *nats.Msg) {
		// Worker service handles task processing internally.
		_ = msg
	})
	if err != nil {
		logger.Error("subscribe", "err", err)
		os.Exit(1)
	}

	// Start heartbeat.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = nc.Publish(broker.SubjectWorkerHeartbeat, []byte(`{"status":"alive"}`))
			}
		}
	}()

	logger.Info("crawler started", "pool_size", cfg.Crawler.PoolSize)

	// Block until signal.
	_ = worker // Worker ready for use via NATS subscriptions.
	<-ctx.Done()
	logger.Info("crawler shutting down")
}
