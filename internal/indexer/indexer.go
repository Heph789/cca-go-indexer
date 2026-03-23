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

// tick performs one iteration of the indexer loop. It determines the next
// block range to process based on the current cursor, chain head, confirmation
// depth, and batch size. Within a single atomic transaction it:
//  1. Records block metadata (hash, parent hash) for each block in the range
//  2. Dispatches matching logs to registered event handlers
//  3. Advances the cursor to the last processed block
//
// Returns the new cursor position, whether the indexer has caught up to the
// safe head (atHead), and any error encountered. When atHead is true, the
// caller should sleep before the next tick to avoid busy-polling.
func (idx *ChainIndexer) tick(ctx context.Context, cursor uint64) (uint64, bool, error) {
	head, err := idx.ethClient.BlockNumber(ctx)
	if err != nil {
		return cursor, false, fmt.Errorf("BlockNumber: %w", err)
	}

	// safeHead is the highest block we consider final. If confirmations
	// exceed the chain head, clamp to 0 to avoid uint64 underflow.
	var safeHead uint64
	if head > idx.config.Confirmations {
		safeHead = head - idx.config.Confirmations
	}

	// Nothing to process — cursor is already at or past the safe head.
	if cursor >= safeHead {
		return cursor, true, nil
	}

	// Compute the batch range [from, to]. Start one block past the cursor
	// and cap at either the batch size limit or the safe head.
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

	// All writes happen atomically: block records, event handler side-effects,
	// and the cursor update succeed or fail together.
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

// Run starts the indexer loop and blocks until ctx is cancelled.
//
// On startup it loads the last persisted cursor from the store. If no cursor
// exists (fresh start), it seeds the cursor to StartBlock-1 so the first tick
// begins processing at StartBlock.
//
// The loop calls tick repeatedly. When tick reports atHead (caught up to the
// safe chain head), Run sleeps for PollInterval before polling again. When
// behind, it loops immediately to catch up as fast as possible.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	cursor, _, err := idx.store.CursorRepo().Get(ctx, idx.chainID)
	if err != nil {
		return fmt.Errorf("CursorRepo.Get: %w", err)
	}
	// On first run with no persisted cursor, back up one block so tick
	// starts processing at exactly StartBlock.
	if cursor == 0 && idx.config.StartBlock > 0 {
		cursor = idx.config.StartBlock - 1
	}

	for {
		// Check for shutdown before each tick.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		newCursor, atHead, err := idx.tick(ctx, cursor)
		if err != nil {
			return err
		}
		cursor = newCursor

		// At head — wait for new blocks before polling again.
		if atHead {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
			}
		}
	}
}
