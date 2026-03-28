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
	"github.com/cca/go-indexer/internal/indexer/handlers"
	applog "github.com/cca/go-indexer/internal/log"
	"github.com/cca/go-indexer/internal/store/postgres"
)

func main() {
	cfg, err := config.LoadIndexer()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	logger := applog.NewLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	ethClient, err := eth.NewClient(cfg.RPCURL)
	if err != nil {
		logger.Error("connecting to rpc", "error", err)
		os.Exit(1)
	}
	defer ethClient.Close()

	registry := indexer.NewRegistry(&handlers.AuctionCreatedHandler{})

	idxConfig := indexer.IndexerConfig{
		ChainID:        cfg.ChainID,
		StartBlock:     cfg.StartBlock,
		PollInterval:   cfg.PollInterval,
		BlockBatchSize: cfg.BlockBatchSize,
		Confirmations:  cfg.Confirmations,
		Addresses:      []common.Address{common.HexToAddress(cfg.FactoryAddr)},
	}

	idx := indexer.New(ethClient, st, registry, idxConfig, logger)

	logger.Info("starting indexer", "chain_id", cfg.ChainID, "start_block", cfg.StartBlock)
	if err := idx.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("indexer stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("indexer stopped gracefully")
}
