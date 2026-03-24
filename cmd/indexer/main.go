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
	"github.com/cca/go-indexer/internal/indexer"
	"github.com/cca/go-indexer/internal/store/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ethClient, err := eth.Dial(cfg.RPCURL)
	if err != nil {
		logger.Error("failed to connect to eth client", "error", err)
		os.Exit(1)
	}
	defer ethClient.Close()

	registry := indexer.NewRegistry(
		&indexer.AuctionCreatedHandler{},
	)

	idxConfig := indexer.IndexerConfig{
		ChainID:        cfg.ChainID,
		StartBlock:     cfg.StartBlock,
		PollInterval:   cfg.PollInterval,
		BlockBatchSize: cfg.BlockBatchSize,
		Confirmations:  cfg.Confirmations,
		Addresses:      []common.Address{common.HexToAddress(cfg.FactoryAddress)},
	}

	idx := indexer.New(ethClient, store, registry, idxConfig, logger)

	logger.Info("starting indexer", "chain_id", cfg.ChainID, "start_block", cfg.StartBlock, "factory", cfg.FactoryAddress)

	if err := idx.Run(ctx); err != nil {
		logger.Error("indexer stopped", "error", err)
		os.Exit(1)
	}
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
