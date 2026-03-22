package indexer

import (
	"context"
	"log/slog"
	"time"

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
	panic("not implemented")
}

// Run starts the indexer loop. It blocks until ctx is cancelled.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	panic("not implemented")
}
