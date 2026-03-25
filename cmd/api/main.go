// Command api starts the CCA indexer REST API server.
//
// Configuration is via environment variables (see internal/config).
// The server supports graceful shutdown — on SIGINT/SIGTERM it drains
// in-flight requests for up to 10 seconds before exiting.
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

	"github.com/cca/go-indexer/internal/api"
	"github.com/cca/go-indexer/internal/config"
	ilog "github.com/cca/go-indexer/internal/log"
	"github.com/cca/go-indexer/internal/store/postgres"
)

func main() {
	// --- Step 1: Load config from environment ---
	// Reuses the indexer config loader. The API only needs a subset of fields
	// (DatabaseURL, ChainID, Port, log settings) but loading the full config
	// keeps everything in one place.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// --- Step 2: Set up structured logger ---
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: ilog.ParseLevel(cfg.LogLevel)}
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)

	// --- Step 3: Connect to PostgreSQL ---
	// The API is read-only — use the read replica URL if configured,
	// otherwise falls back to the primary DATABASE_URL.
	// Migrations are owned by the indexer process, not the API.
	ctx := context.Background()
	store, err := postgres.New(ctx, cfg.DatabaseReadURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// --- Step 4: Create and start the HTTP server ---
	srv := api.NewServer(api.ServerConfig{
		Port:    cfg.Port,
		ChainID: cfg.ChainID,
	}, store, logger)

	go func() {
		logger.Info("server starting", "port", cfg.Port, "chain_id", cfg.ChainID)
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// --- Step 5: Wait for shutdown signal ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("received shutdown signal", "signal", sig.String())

	// --- Step 6: Graceful shutdown with 10s deadline ---
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
