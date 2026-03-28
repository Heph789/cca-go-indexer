package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"golang.org/x/sync/errgroup"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// IndexerConfig holds the parameters that control a ChainIndexer's polling behavior.
type IndexerConfig struct {
	ChainID            int64
	StartBlock         uint64
	PollInterval       time.Duration
	BlockBatchSize     uint64
	Confirmations      uint64
	HeaderConcurrency  int
	Addresses          []common.Address
}

const maxLoopRetries = 5

type blockHeader struct {
	hash       common.Hash
	parentHash common.Hash
}

// ChainIndexer polls an EVM chain for logs, dispatches them through a
// HandlerRegistry, and persists block/cursor progress in a Store.
type ChainIndexer struct {
	ethClient eth.Client
	store     store.Store
	registry  *HandlerRegistry
	config    IndexerConfig
	logger    *slog.Logger
}

// New constructs a ChainIndexer with the given dependencies and config.
func New(ethClient eth.Client, s store.Store, registry *HandlerRegistry, config IndexerConfig, logger *slog.Logger) *ChainIndexer {
	return &ChainIndexer{
		ethClient: ethClient,
		store:     s,
		registry:  registry,
		config:    config,
		logger:    logger.With("chain_id", config.ChainID),
	}
}

func (idx *ChainIndexer) handleLoopError(consecutiveErrors *int, operation string, err error) bool {
	*consecutiveErrors++
	exhausted := *consecutiveErrors >= maxLoopRetries
	msg := "transient error, will retry"
	if exhausted {
		msg = "retry budget exhausted"
	}
	idx.logger.Error(msg,
		"operation", operation,
		"error", err,
		"attempt", *consecutiveErrors,
		"max_retries", maxLoopRetries,
	)
	return exhausted
}

func (idx *ChainIndexer) Run(ctx context.Context) error {
	cursor, _, err := idx.store.CursorRepo().Get(ctx, idx.config.ChainID)
	if err != nil {
		return fmt.Errorf("loading cursor: %w", err)
	}

	if cursor == 0 && idx.config.StartBlock > 0 {
		cursor = idx.config.StartBlock - 1
	}

	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chainHead, err := idx.ethClient.BlockNumber(ctx)
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "get block number", err); shouldExit {
				return fmt.Errorf("getting chain head (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		if chainHead < idx.config.Confirmations {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		if chainHead < idx.config.Confirmations {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		safeHead := chainHead - idx.config.Confirmations
		if cursor >= safeHead {
			idx.logger.Debug("at chain head, sleeping", "cursor", cursor, "safe_head", safeHead)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		// Reorg detection
		reorged, err := detectReorg(ctx, idx.ethClient, idx.store.BlockRepo(), idx.config.ChainID, cursor)
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

		if reorged {
			ancestor, err := handleReorg(ctx, idx.logger, idx.ethClient, idx.store, idx.config.ChainID, cursor)
			if err != nil {
				return fmt.Errorf("handle reorg: %w", err)
			}
			cursor = ancestor
			consecutiveErrors = 0
			continue
		}

		from := cursor + 1
		to := min(cursor+idx.config.BlockBatchSize, safeHead)

		idx.logger.Info("processing batch", "from", from, "to", to)

		logs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: idx.config.Addresses,
			Topics:    idx.registry.TopicFilter(),
		})
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "filter logs", err); shouldExit {
				return fmt.Errorf("filtering logs (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		concurrency := idx.config.HeaderConcurrency
		if concurrency <= 0 {
			concurrency = 1
		}

		headers := make([]blockHeader, to-from+1)

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)

		for block := from; block <= to; block++ {
			g.Go(func() error {
				header, err := idx.ethClient.HeaderByNumber(gCtx, new(big.Int).SetUint64(block))
				if err != nil {
					return fmt.Errorf("getting header for block %d: %w", block, err)
				}
				headers[block-from] = blockHeader{
					hash:       header.Hash(),
					parentHash: header.ParentHash,
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "fetch headers", err); shouldExit {
				return fmt.Errorf("fetching headers (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		lastBlockHash := headers[to-from].hash

		err = idx.store.WithTx(ctx, func(txStore store.Store) error {
			for _, log := range logs {
				if err := idx.registry.HandleLog(ctx, idx.config.ChainID, log, txStore); err != nil {
					return fmt.Errorf("handling log: %w", err)
				}
			}

			for i, h := range headers {
				block := from + uint64(i)
				if err := txStore.BlockRepo().Insert(ctx, idx.config.ChainID, block, h.hash, h.parentHash); err != nil {
					return fmt.Errorf("inserting block %d: %w", block, err)
				}
			}

			if err := txStore.CursorRepo().Upsert(ctx, idx.config.ChainID, to, lastBlockHash); err != nil {
				return fmt.Errorf("upserting cursor: %w", err)
			}

			return nil
		})
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "execute batch tx", err); shouldExit {
				return fmt.Errorf("executing batch tx (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		cursor = to
		consecutiveErrors = 0
	}
}
