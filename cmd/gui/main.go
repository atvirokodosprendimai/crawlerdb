package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/gui"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
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

	// Open database.
	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.Error("get sql.DB", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// Connect to NATS.
	nc, err := nats.Connect(cfg.NATS.URL,
		nats.Name("crawlerdb-gui"),
	)
	if err != nil {
		logger.Error("connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	mb := broker.NewFromConn(nc)

	// Create router.
	router := gui.NewRouter(db, mb, logger)

	srv := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: router,
	}
	srv.RegisterOnShutdown(func() {
		if err := router.Close(); err != nil {
			logger.Warn("close gui router", "err", err)
		}
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("GUI server started", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("GUI shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown timed out, forcing close", "err", err)
		_ = srv.Close()
	}
}
