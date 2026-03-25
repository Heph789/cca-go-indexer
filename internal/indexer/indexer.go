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

// maxLoopRetries is the number of times the indexer retries a failed
// batch before giving up and exiting. Each retry sleeps for PollInterval.
// Combined with the transport-level retry budget (~24s with defaults),
// this gives the indexer ~2 minutes of tolerance for sustained outages
// before crashing and relying on process supervision.
const maxLoopRetries = 5

// IndexerConfig holds the runtime parameters for a ChainIndexer.
type IndexerConfig struct {
	ChainID        int64
	StartBlock     uint64           // fallback if no cursor exists in DB
	PollInterval   time.Duration    // sleep duration when at chain head
	BlockBatchSize uint64           // max blocks per eth_getLogs call
	Confirmations  uint64           // blocks behind head to treat as "safe"
	Addresses      []common.Address // contract addresses to watch
}

// ChainIndexer runs the main indexing loop for a single chain.
// Each chain gets its own goroutine with an independent cursor.
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

// Run starts the indexer loop. It blocks until ctx is cancelled.
// The loop is fully resumable — on restart it picks up from the
// cursor stored in the database.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	// --- Step 1: Load cursor from DB, or use StartBlock ---
	cursor, cursorHash, err := idx.store.CursorRepo().Get(ctx, idx.chainID)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}
	if cursor == 0 && idx.config.StartBlock > 0 {
		cursor = idx.config.StartBlock - 1 // will process StartBlock first
		idx.logger.Info("no cursor found, starting from config", "start_block", idx.config.StartBlock)
	} else {
		idx.logger.Info("resuming from cursor", "block", cursor, "hash", cursorHash)
	}

	// --- Step 2: Main loop ---
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			idx.logger.Info("shutting down")
			return ctx.Err()
		default:
		}

		// 2a. Get chain head from RPC.
		chainHead, err := idx.ethClient.BlockNumber(ctx)
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "get block number", err); shouldExit {
				return fmt.Errorf("get block number (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// 2b. Apply confirmations to get safe head.
		if chainHead < idx.config.Confirmations {
			idx.logger.Debug("chain too young for confirmation buffer, sleeping", "head", chainHead, "confirmations", idx.config.Confirmations)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}
		safeHead := chainHead - idx.config.Confirmations

		// 2c. If at head, sleep and retry.
		if cursor >= safeHead {
			idx.logger.Debug("at chain head, sleeping", "cursor", cursor, "head", safeHead)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// 2d. Calculate batch range.
		from := cursor + 1
		to := min(cursor+idx.config.BlockBatchSize, safeHead)

		// 2e. Check for reorg at from-1 (the block we already processed).
		reorged, err := detectReorg(ctx, idx.ethClient, idx.store.BlockRepo(), idx.chainID, cursor)
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "detect reorg", err); shouldExit {
				return fmt.Errorf("detect reorg at block %d (after %d retries): %w", cursor, maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// 2f. If reorg detected, roll back and reset cursor.
		if reorged {
			ancestor, err := handleReorg(ctx, idx.logger, idx.ethClient, idx.store, idx.chainID, cursor)
			if err != nil {
				return fmt.Errorf("handle reorg: %w", err)
			}
			cursor = ancestor
			consecutiveErrors = 0
			continue // re-enter loop with new cursor
		}

		// 2g. Fetch logs for the batch range.
		logs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: idx.config.Addresses,
			Topics:    idx.registry.TopicFilter(),
		})
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "filter logs", err); shouldExit {
				return fmt.Errorf("filter logs [%d, %d] (after %d retries): %w", from, to, maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// 2h. Fetch block headers for hash tracking.
		// We need the hash and parent hash for each block in the range
		// to support reorg detection on the next iteration.
		type blockInfo struct {
			number     uint64
			hash       string
			parentHash string
		}
		blocks := make([]blockInfo, 0, to-from+1)
		headerFailed := false
		for bn := from; bn <= to; bn++ {
			header, err := idx.ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(bn))
			if err != nil {
				if shouldExit := idx.handleLoopError(&consecutiveErrors, "get block header", err); shouldExit {
					return fmt.Errorf("get block header %d (after %d retries): %w", bn, maxLoopRetries, err)
				}
				headerFailed = true
				break
			}
			blocks = append(blocks, blockInfo{
				number:     bn,
				hash:       header.Hash().Hex(),
				parentHash: header.ParentHash.Hex(),
			})
		}
		if headerFailed {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// 2i. Atomic write: all events + block hashes + cursor update
		// in a single DB transaction.
		lastBlockHash := ""
		if len(blocks) > 0 {
			lastBlockHash = blocks[len(blocks)-1].hash
		}

		err = idx.store.WithTx(ctx, func(txStore store.Store) error {
			// Process each log through the handler registry.
			for _, log := range logs {
				if err := idx.registry.HandleLog(ctx, idx.chainID, log, txStore); err != nil {
					return fmt.Errorf("handle log (block=%d, tx=%s, idx=%d): %w",
						log.BlockNumber, log.TxHash.Hex(), log.Index, err)
				}
			}

			// Record block hashes for reorg detection.
			for _, b := range blocks {
				if err := txStore.BlockRepo().Insert(ctx, idx.chainID, b.number, b.hash, b.parentHash); err != nil {
					return fmt.Errorf("insert block %d: %w", b.number, err)
				}
			}

			// Advance cursor to the end of this batch.
			if err := txStore.CursorRepo().Upsert(ctx, idx.chainID, to, lastBlockHash); err != nil {
				return fmt.Errorf("update cursor: %w", err)
			}

			return nil
		})
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "process batch", err); shouldExit {
				return fmt.Errorf("process batch [%d, %d] (after %d retries): %w", from, to, maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		idx.logger.Info("indexed batch", "from", from, "to", to, "logs", len(logs))
		cursor = to
		consecutiveErrors = 0

		// 2j. If still behind head, continue immediately (catching up).
		// Otherwise the loop will check and sleep at step 2c.
	}
}

// handleLoopError logs a transient error and increments the consecutive
// error counter. Returns true if the retry budget is exhausted and the
// caller should exit.
func (idx *ChainIndexer) handleLoopError(consecutiveErrors *int, operation string, err error) bool {
	*consecutiveErrors++
	idx.logger.Error("transient error, will retry",
		"operation", operation,
		"error", err,
		"attempt", *consecutiveErrors,
		"max_retries", maxLoopRetries,
	)
	return *consecutiveErrors >= maxLoopRetries
}
