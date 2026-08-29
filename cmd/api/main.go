// Command api serves the ingress and management surfaces of vhook. It applies
// pending migrations at boot under a Postgres advisory lock, then serves HTTP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/victorzix/vhook/internal/obs"
	"github.com/victorzix/vhook/internal/store"
)

// Injected at build time with -ldflags.
var (
	version = "dev"
	commit  = "none"
)

// drainGrace is how long readiness reports 503 before the server stops
// accepting connections, so a load balancer notices before requests are cut.
const drainGrace = 5 * time.Second

func main() {
	logger := obs.NewLogger(os.Stdout, slog.LevelInfo)
	if err := run(logger); err != nil {
		logger.Error("shutting down after failure", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations before anything else, and before the port opens: a process
	// that serves against an outdated schema fails in ways that look like
	// application bugs.
	if err := store.Migrate(ctx, cfg.databaseURL); err != nil {
		return err
	}

	pool, err := store.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	obs.RegisterBuildInfo(version, commit)

	// Check order decides which code leads a 503, so it is fixed here.
	health := obs.NewHealth(logger, postgresCheck(pool), rabbitCheck(cfg.rabbitURL))

	router, err := buildRouter(logger, health, pool, cfg)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.httpAddr, "version", version)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("draining", "grace", drainGrace.String())
	health.Drain()
	time.Sleep(drainGrace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
