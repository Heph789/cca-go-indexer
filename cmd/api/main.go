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
	"github.com/cca/go-indexer/internal/api/handlers"
	"github.com/cca/go-indexer/internal/config"
	applog "github.com/cca/go-indexer/internal/log"
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
		Port: cfg.Port,
	}, appMux, healthMux, logger)

	go func() {
		logger.Info("starting api server", "port", cfg.Port)
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down api server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}
	logger.Info("api server stopped gracefully")
}
