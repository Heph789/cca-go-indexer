package indexer

import (
	"context"
	"log/slog"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

const maxReorgDepth = 128

// detectReorg checks whether the chain's hash for the given block
// matches what we stored. Returns true if there's a mismatch.
func detectReorg(ctx context.Context, ethClient eth.Client, blockRepo store.BlockRepository, chainID int64, blockNumber uint64) (bool, error) {
	panic("not implemented")
}

// handleReorg walks back from reorgBlock to find the common ancestor,
// then atomically rolls back all data after the ancestor.
// Returns the ancestor block number.
func handleReorg(ctx context.Context, logger *slog.Logger, ethClient eth.Client, s store.Store, chainID int64, reorgBlock uint64) (uint64, error) {
	panic("not implemented")
}
