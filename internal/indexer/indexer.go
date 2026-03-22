package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// IndexerConfig holds the runtime parameters for a ChainIndexer.
type IndexerConfig struct {
	ChainID        int64
	StartBlock     uint64
	PollInterval   time.Duration
	BlockBatchSize uint64
	Confirmations  uint64
	Addresses      []common.Address
}

// ChainIndexer runs the main indexing loop for a single chain.
type ChainIndexer struct {
	chainID   int64
	ethClient eth.Client
	store     store.Store
	registry  *HandlerRegistry
	config    IndexerConfig
	logger    *slog.Logger
}

// New creates a new ChainIndexer.
func New(ethClient eth.Client, s store.Store, registry *HandlerRegistry, config IndexerConfig, logger *slog.Logger) *ChainIndexer {
	return &ChainIndexer{
		chainID:   config.ChainID,
		ethClient: ethClient,
		store:     s,
		registry:  registry,
		config:    config,
		logger:    logger.With("chain_id", config.ChainID),
	}
}

// tick performs one iteration of the indexer loop.
// Returns (newCursor, atHead, error).
func (idx *ChainIndexer) tick(ctx context.Context, cursor uint64) (uint64, bool, error) {
	head, err := idx.ethClient.BlockNumber(ctx)
	if err != nil {
		return cursor, false, fmt.Errorf("BlockNumber: %w", err)
	}

	var safeHead uint64
	if head > idx.config.Confirmations {
		safeHead = head - idx.config.Confirmations
	}

	if cursor >= safeHead {
		return cursor, true, nil
	}

	from := cursor + 1
	to := cursor + idx.config.BlockBatchSize
	if to > safeHead {
		to = safeHead
	}

	logs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: idx.config.Addresses,
		Topics:    idx.registry.TopicFilter(),
	})
	if err != nil {
		return cursor, false, fmt.Errorf("FilterLogs: %w", err)
	}

	err = idx.store.WithTx(ctx, func(txStore store.Store) error {
		for blockNum := from; blockNum <= to; blockNum++ {
			header, err := idx.ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNum))
			if err != nil {
				return fmt.Errorf("HeaderByNumber(%d): %w", blockNum, err)
			}
			if err := txStore.BlockRepo().Insert(ctx, idx.chainID, blockNum, header.Hash().Hex(), header.ParentHash.Hex()); err != nil {
				return fmt.Errorf("BlockRepo.Insert(%d): %w", blockNum, err)
			}
		}

		for _, log := range logs {
			if err := idx.registry.HandleLog(ctx, idx.chainID, log, txStore); err != nil {
				return fmt.Errorf("HandleLog: %w", err)
			}
		}

		lastHeader, err := idx.ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(to))
		if err != nil {
			return fmt.Errorf("HeaderByNumber(%d): %w", to, err)
		}
		if err := txStore.CursorRepo().Upsert(ctx, idx.chainID, to, lastHeader.Hash().Hex()); err != nil {
			return fmt.Errorf("CursorRepo.Upsert: %w", err)
		}

		return nil
	})
	if err != nil {
		return cursor, false, err
	}

	return to, to >= safeHead, nil
}

// Run starts the indexer loop. It blocks until ctx is cancelled.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	panic("not implemented")
}
