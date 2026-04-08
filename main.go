package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/phekno/ebay-watcher/internal/config"
	"github.com/phekno/ebay-watcher/internal/notifier"
	"github.com/phekno/ebay-watcher/internal/scheduler"
	"github.com/phekno/ebay-watcher/internal/server"
	"github.com/phekno/ebay-watcher/internal/store"
	"github.com/phekno/ebay-watcher/internal/watcher"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := store.New(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	n := notifier.NewDiscord(cfg.DiscordWebhookURL)
	w := watcher.New(cfg.EbayClientID, cfg.EbaySecret, db, n)
	s := scheduler.New(cfg.PollInterval, w.Run)

	srv := server.New(cfg, db)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: srv.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("ebay-watcher starting",
		"listen", cfg.ListenAddr,
		"poll_interval", cfg.PollInterval,
	)

	// HTTP server in background
	go func() {
		slog.Info("http server listening", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	// Watcher scheduler (blocks until ctx cancelled)
	s.Start(ctx)

	slog.Info("shutting down http server")
	if err := httpServer.Shutdown(context.Background()); err != nil {
		slog.Error("http server shutdown error", "error", err)
	}
	slog.Info("ebay-watcher stopped")
}
