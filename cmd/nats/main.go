package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	"github.com/nats-io/nats-server/v2/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := config.LoadDefault()
	if cfgFile := os.Getenv("CRAWLERDB_CONFIG"); cfgFile != "" {
		if loaded, err := config.LoadFromFile(cfgFile); err == nil {
			cfg = loaded
		} else {
			logger.Warn("failed to load config, using defaults", "err", err)
		}
	}

	opts, bindAddr, err := optionsFromURL(cfg.NATS.URL)
	if err != nil {
		logger.Error("invalid NATS URL", "url", cfg.NATS.URL, "err", err)
		os.Exit(1)
	}
	opts.ServerName = "crawlerdb-nats"
	opts.NoSigs = true
	opts.JetStream = true
	opts.StoreDir = cfg.NATS.JetStreamDir

	srv, err := server.NewServer(opts)
	if err != nil {
		logger.Error("create embedded NATS server", "err", err)
		os.Exit(1)
	}

	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		logger.Error("embedded NATS server did not become ready", "bind", bindAddr)
		os.Exit(1)
	}

	logger.Info("embedded NATS started", "bind", bindAddr)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	<-ctx.Done()
	logger.Info("embedded NATS shutting down", "bind", bindAddr)
	srv.Shutdown()
	srv.WaitForShutdown()
}

func optionsFromURL(rawURL string) (*server.Options, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "nats" {
		return nil, "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}

	port := 4222
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return nil, "", fmt.Errorf("parse port: %w", err)
		}
		port = parsedPort
	}

	return &server.Options{
		Host: host,
		Port: port,
	}, fmt.Sprintf("%s:%d", host, port), nil
}
