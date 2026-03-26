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

type IndexerConfig struct {
	ChainID            int64
	StartBlock         uint64
	PollInterval       time.Duration
	BlockBatchSize     uint64
	Confirmations      uint64
	HeaderConcurrency  int
	Addresses          []common.Address
}

type blockHeader struct {
	hash       string
	parentHash string
}

type ChainIndexer struct {
	ethClient eth.Client
	store     store.Store
	registry  *HandlerRegistry
	config    IndexerConfig
	logger    *slog.Logger
}

func New(ethClient eth.Client, s store.Store, registry *HandlerRegistry, config IndexerConfig, logger *slog.Logger) *ChainIndexer {
	return &ChainIndexer{
		ethClient: ethClient,
		store:     s,
		registry:  registry,
		config:    config,
		logger:    logger.With("chain_id", config.ChainID),
	}
}

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

		safeHead := chainHead - idx.config.Confirmations
		if cursor >= safeHead {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idx.config.PollInterval):
				continue
			}
		}

		from := cursor + 1
		to := min(cursor+idx.config.BlockBatchSize, safeHead)

		logs, err := idx.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: idx.config.Addresses,
			Topics:    idx.registry.TopicFilter(),
		})
		if err != nil {
			return fmt.Errorf("filtering logs: %w", err)
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
					hash:       header.Hash().Hex(),
					parentHash: header.ParentHash.Hex(),
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return fmt.Errorf("fetching headers: %w", err)
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
			return fmt.Errorf("executing batch tx: %w", err)
		}

		cursor = to
	}
}
