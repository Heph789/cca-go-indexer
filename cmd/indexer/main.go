// Command indexer is the entrypoint for the CCA event indexer.
// It wires together config, database, RPC client, event handlers,
// and the chain indexer loop, then runs until interrupted.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/config"
	"github.com/cca/go-indexer/internal/eth"
	ilog "github.com/cca/go-indexer/internal/log"
	"github.com/cca/go-indexer/internal/indexer"
	"github.com/cca/go-indexer/internal/indexer/handlers"
	"github.com/cca/go-indexer/internal/store/postgres"
)

func main() {
	// --- Step 1: Load config from environment ---
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

	// --- Step 3: Connect to PostgreSQL and run migrations ---
	ctx := context.Background()
	store, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// --- Step 4: Create Ethereum RPC client ---
	ethClient, err := eth.NewClient(cfg.RPCURL, eth.RetryConfig{
		MaxRetries: cfg.RPCMaxRetries,
		BaseDelay:  cfg.RPCRetryDelay,
	})
	if err != nil {
		logger.Error("failed to create eth client", "error", err)
		os.Exit(1)
	}
	defer ethClient.Close()

	// --- Step 5: Register event handlers ---
	// Handlers are grouped by the contract that emits them.
	registry := indexer.NewRegistry(
		// CCA Factory (ccaf)
		&handlers.AuctionCreatedHandler{},

		// Future: CCA Auction (ccaa)
		// &handlers.BidSubmittedHandler{},
		// &handlers.ClearingPriceHandler{},
	)

	// --- Step 6: Create and run chain indexer ---
	// Note(@chase): this is fine for MVP, but the eventual goal is to be able to register multiple chains
	idxConfig := indexer.IndexerConfig{
		ChainID:        cfg.ChainID,
		StartBlock:     cfg.StartBlock,
		PollInterval:   cfg.PollInterval,
		BlockBatchSize: cfg.BlockBatchSize,
		Confirmations:  cfg.Confirmations,
		Addresses:      []common.Address{common.HexToAddress(cfg.FactoryAddr)},
	}

	chainIndexer := indexer.New(ethClient, store, registry, idxConfig, logger)

	// --- Step 7: Handle OS signals for graceful shutdown ---
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("starting indexer",
		"chain_id", cfg.ChainID,
		"factory", cfg.FactoryAddr,
		"start_block", cfg.StartBlock,
	)

	if err := chainIndexer.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("indexer failed", "error", err)
		os.Exit(1)
	}

	logger.Info("indexer stopped")
}
