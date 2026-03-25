package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// maxReorgDepth is the safety cap on how far back the indexer will
// walk when looking for a common ancestor during a reorg.
// If the reorg is deeper than this, the indexer halts with an error.
const maxReorgDepth = 128

// detectReorg checks whether the chain's hash for the block before
// the new range matches what we stored. Returns true if there's a
// mismatch (reorg detected).
func detectReorg(ctx context.Context, ethClient eth.Client, blockRepo store.BlockRepository, chainID int64, blockNumber uint64) (bool, error) {
	if blockNumber == 0 {
		return false, nil // nothing to compare against at genesis
	}

	// Get the hash we stored for this block.
	storedHash, err := blockRepo.GetHash(ctx, chainID, blockNumber)
	if err != nil {
		return false, fmt.Errorf("get stored hash for block %d: %w", blockNumber, err)
	}
	if storedHash == "" {
		return false, nil // no stored hash means we haven't indexed this block yet
	}

	// Get the chain's current hash for this block.
	header, err := ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return false, fmt.Errorf("get chain hash for block %d: %w", blockNumber, err)
	}

	return storedHash != header.Hash().Hex(), nil
}

// handleReorg walks backwards from reorgBlock to find the common
// ancestor (last block where stored hash matches chain hash), then
// rolls back all data after that block in a single transaction.
//
// Returns the common ancestor block number, which becomes the new cursor.
func handleReorg(ctx context.Context, logger *slog.Logger, ethClient eth.Client, s store.Store, chainID int64, reorgBlock uint64) (uint64, error) {
	logger.Warn("reorg detected", "chain_id", chainID, "at_block", reorgBlock)

	// --- Step 1: Walk backwards to find common ancestor ---
	ancestor := reorgBlock
	for depth := uint64(0); depth < maxReorgDepth; depth++ {
		if ancestor == 0 {
			break
		}
		ancestor--

		storedHash, err := s.BlockRepo().GetHash(ctx, chainID, ancestor)
		if err != nil {
			return 0, fmt.Errorf("get stored hash for block %d: %w", ancestor, err)
		}

		header, err := ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(ancestor))
		if err != nil {
			return 0, fmt.Errorf("get chain header for block %d: %w", ancestor, err)
		}

		if storedHash == header.Hash().Hex() {
			logger.Info("found common ancestor", "block", ancestor)
			break
		}
	}

	// Safety check: if we walked back maxReorgDepth without finding
	// agreement, something is very wrong. Halt rather than silently
	// deleting large amounts of data.
	if reorgBlock-ancestor >= maxReorgDepth {
		return 0, fmt.Errorf("reorg deeper than %d blocks — manual intervention required", maxReorgDepth)
	}

	// --- Step 2: Atomic rollback of all data after common ancestor ---
	rollbackFrom := ancestor + 1
	logger.Info("rolling back", "from_block", rollbackFrom, "chain_id", chainID)

	err := s.WithTx(ctx, func(txStore store.Store) error {
		if err := txStore.RawEventRepo().DeleteFromBlock(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete raw events: %w", err)
		}
		if err := txStore.AuctionRepo().DeleteFromBlock(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete event_ccaf_auction_created: %w", err)
		}
		if err := txStore.BlockRepo().DeleteFrom(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete blocks: %w", err)
		}

		// Get the ancestor block hash for cursor update.
		ancestorHash, err := txStore.BlockRepo().GetHash(ctx, chainID, ancestor)
		if err != nil {
			return fmt.Errorf("get ancestor hash: %w", err)
		}

		if err := txStore.CursorRepo().Upsert(ctx, chainID, ancestor, ancestorHash); err != nil {
			return fmt.Errorf("update cursor: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("rollback transaction: %w", err)
	}

	return ancestor, nil
}
