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

	// Internal state cached across ticks.
	cursor      uint64
	cursorHash  string
	initialized bool
}

// New creates a new ChainIndexer.
func New(ethClient eth.Client, s store.Store, registry *HandlerRegistry, config IndexerConfig, logger *slog.Logger) *ChainIndexer {
	panic("not implemented")
}

// Run starts the indexer loop. Blocks until ctx is cancelled.
func (idx *ChainIndexer) Run(ctx context.Context) error {
	panic("not implemented")
}

// tick performs one iteration of the indexer loop.
// Returns true if the indexer is at head and the caller should sleep.
func (idx *ChainIndexer) tick(ctx context.Context) (atHead bool, err error) {
	panic("not implemented")
}
