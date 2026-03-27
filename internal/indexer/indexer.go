package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// IndexerConfig holds the parameters that control a ChainIndexer's polling behavior.
type IndexerConfig struct {
	ChainID        int64
	StartBlock     uint64
	PollInterval   time.Duration
	BlockBatchSize uint64
	Confirmations  uint64
	Addresses      []common.Address
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

// Run starts the main polling loop. It resumes from the stored cursor (or
// StartBlock), fetches logs in batches up to the safe chain head, dispatches
// them through the registry, and advances the cursor. It blocks until the
// context is cancelled or a fatal error occurs.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	cursor, _, err := idx.store.CursorRepo().Get(ctx, idx.config.ChainID)
	if err != nil {
		return fmt.Errorf("loading cursor: %w", err)
	}

	if cursor == 0 && idx.config.StartBlock > 0 {
		cursor = idx.config.StartBlock - 1
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chainHead, err := idx.ethClient.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("getting chain head: %w", err)
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
			return fmt.Errorf("filtering logs: %w", err)
		}

		headers := make(map[uint64]struct{ hash, parentHash string })
		for block := from; block <= to; block++ {
			header, err := idx.ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(block))
			if err != nil {
				return fmt.Errorf("getting header for block %d: %w", block, err)
			}
			blockHash := header.Hash().Hex()
			parentHash := header.ParentHash.Hex()
			headers[block] = struct{ hash, parentHash string }{blockHash, parentHash}
		}

		lastBlockHash := headers[to].hash

		err = idx.store.WithTx(ctx, func(txStore store.Store) error {
			for _, log := range logs {
				if err := idx.registry.HandleLog(ctx, idx.config.ChainID, log, txStore); err != nil {
					return fmt.Errorf("handling log: %w", err)
				}
			}

			for block := from; block <= to; block++ {
				h := headers[block]
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
			return fmt.Errorf("executing batch tx: %w", err)
		}

		cursor = to
	}
}
