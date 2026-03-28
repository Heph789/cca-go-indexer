package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cca/go-indexer/internal/api"
	"github.com/cca/go-indexer/internal/api/handlers"
	"github.com/cca/go-indexer/internal/config"
	applog "github.com/cca/go-indexer/internal/log"
	"github.com/cca/go-indexer/internal/store"
	"github.com/cca/go-indexer/internal/store/postgres"
)

func main() {
	cfg, err := config.LoadAPI()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	logger := applog.NewLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := postgres.New(ctx, cfg.DatabaseReadURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}

	// run owns the server lifecycle and defers st.Close(), so all cleanup
	// executes even when the server goroutine fails.
	if err := run(ctx, cancel, cfg, logger, st); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// run encapsulates the API server lifecycle so that deferred cleanup (e.g.
// closing the store) always executes, even when the server goroutine fails.
// Errors are returned to the caller instead of calling os.Exit directly.
func run(ctx context.Context, cancel context.CancelFunc, cfg *config.Config, logger *slog.Logger, st store.Store) error {
	defer st.Close()

	auctionHandler := &handlers.AuctionHandler{Store: st, ChainID: cfg.ChainID}
	healthHandler := &handlers.HealthHandler{Store: st, Logger: logger}

	// Application routes go through the middleware chain.
	appMux := http.NewServeMux()
	appMux.HandleFunc("GET /api/v1/auctions/{address}", auctionHandler.Get)

	// Health probes bypass the middleware chain so they stay fast,
	// dependency-free, and never produce request logs that drown out
	// real traffic.
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("GET /health", healthHandler.Health)
	healthMux.HandleFunc("GET /ready", healthHandler.Ready)

	srv := api.NewServer(api.ServerConfig{
		Host: cfg.Host,
		Port: cfg.Port,
	}, appMux, healthMux, logger)

	// errCh communicates a server start failure from the goroutine back to
	// the main flow so we can return it instead of calling os.Exit.
	errCh := make(chan error, 1)

	go func() {
		logger.Info("starting api server", "port", cfg.Port)
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			// Cancel the context so the select below unblocks immediately
			// rather than waiting for an OS signal that will never arrive.
			cancel()
		}
	}()

	// Wait for either a signal (context cancelled) or a server startup error.
	// Using select lets us distinguish between a clean shutdown request and a
	// goroutine failure without any timing assumptions.
	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down api server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	logger.Info("api server stopped gracefully")
	return nil
}
