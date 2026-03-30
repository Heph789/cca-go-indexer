package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"golang.org/x/sync/errgroup"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// IndexerConfig holds the parameters that control a ChainIndexer's polling behavior.
type IndexerConfig struct {
	ChainID           int64
	StartBlock        uint64
	PollInterval      time.Duration
	BlockBatchSize    uint64
	Confirmations     uint64
	HeaderConcurrency int
	Addresses         []common.Address
}

const maxLoopRetries = 5

type blockHeader struct {
	hash       common.Hash
	parentHash common.Hash
	time       uint64
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

		caughtUp, err := idx.store.WatchedContractRepo().ListCaughtUp(ctx, idx.config.ChainID, cursor)
		if err != nil {
			if shouldExit := idx.handleLoopError(&consecutiveErrors, "list caught-up contracts", err); shouldExit {
				return fmt.Errorf("listing caught-up contracts (after %d retries): %w", maxLoopRetries, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		allAddresses := make([]common.Address, 0, len(idx.config.Addresses)+len(caughtUp))
		allAddresses = append(allAddresses, idx.config.Addresses...)
		allAddresses = append(allAddresses, caughtUp...)

		logs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: allAddresses,
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
					time:       header.Time,
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

		blockTimes := make(map[uint64]time.Time, len(headers))
		for i, h := range headers {
			block := from + uint64(i)
			blockTimes[block] = time.Unix(int64(h.time), 0).UTC()
		}

		err = idx.store.WithTx(ctx, func(txStore store.Store) error {
			if err := idx.registry.HandleLogs(ctx, idx.config.ChainID, logs, blockTimes, txStore); err != nil {
				return fmt.Errorf("handling logs: %w", err)
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

			for _, addr := range caughtUp {
				if err := txStore.WatchedContractRepo().UpdateLastIndexedBlock(ctx, idx.config.ChainID, addr, to); err != nil {
					return fmt.Errorf("updating watched contract cursor for %s: %w", addr.Hex(), err)
				}
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

		if err := idx.backfillContracts(ctx, cursor); err != nil {
			idx.logger.Warn("backfill pass", "error", err)
		}
	}
}

// backfillContracts processes one batch of historical blocks for each contract
// that is behind the global cursor. It yields after one batch per contract so
// forward polling is not blocked.
func (idx *ChainIndexer) backfillContracts(ctx context.Context, globalCursor uint64) error {
	contracts, err := idx.store.WatchedContractRepo().ListNeedingBackfill(ctx, idx.config.ChainID, globalCursor)
	if err != nil {
		return fmt.Errorf("listing contracts for backfill: %w", err)
	}

	for _, contract := range contracts {
		if err := idx.backfillContract(ctx, contract, globalCursor); err != nil {
			idx.logger.Warn("backfill contract",
				"error", err,
				"address", contract.Address.Hex(),
			)
		}
	}
	return nil
}

func (idx *ChainIndexer) backfillContract(ctx context.Context, contract *cca.WatchedContract, globalCursor uint64) error {
	backfillFrom := contract.LastIndexedBlock + 1
	if contract.LastIndexedBlock == 0 {
		backfillFrom = contract.StartBlock
	}
	backfillTo := min(backfillFrom+idx.config.BlockBatchSize-1, globalCursor)

	idx.logger.Info("backfilling contract",
		"address", contract.Address.Hex(),
		"from", backfillFrom,
		"to", backfillTo,
	)

	bfLogs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(backfillFrom),
		ToBlock:   new(big.Int).SetUint64(backfillTo),
		Addresses: []common.Address{contract.Address},
		Topics:    idx.registry.TopicFilter(),
	})
	if err != nil {
		return fmt.Errorf("filter logs: %w", err)
	}

	blockTimes, err := idx.fetchBlockTimesForLogs(ctx, bfLogs)
	if err != nil {
		return fmt.Errorf("fetch block times: %w", err)
	}

	return idx.store.WithTx(ctx, func(txStore store.Store) error {
		if err := idx.registry.HandleLogs(ctx, idx.config.ChainID, bfLogs, blockTimes, txStore); err != nil {
			return fmt.Errorf("handling backfill logs: %w", err)
		}
		if err := txStore.WatchedContractRepo().UpdateLastIndexedBlock(ctx, idx.config.ChainID, contract.Address, backfillTo); err != nil {
			return fmt.Errorf("updating backfill cursor for %s: %w", contract.Address.Hex(), err)
		}
		return nil
	})
}

// fetchBlockTimesForLogs retrieves the timestamp for each unique block
// referenced in the given logs.
func (idx *ChainIndexer) fetchBlockTimesForLogs(ctx context.Context, logs []types.Log) (map[uint64]time.Time, error) {
	blockTimes := make(map[uint64]time.Time)
	for _, l := range logs {
		if _, ok := blockTimes[l.BlockNumber]; ok {
			continue
		}
		header, err := idx.ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(l.BlockNumber))
		if err != nil {
			return nil, fmt.Errorf("header for block %d: %w", l.BlockNumber, err)
		}
		blockTimes[l.BlockNumber] = time.Unix(int64(header.Time), 0).UTC()
	}
	return blockTimes, nil
}
