package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

const maxReorgDepth = 128

func detectReorg(ctx context.Context, ethClient eth.Client, blockRepo store.BlockRepository, chainID int64, blockNumber uint64) (bool, error) {
	if blockNumber == 0 {
		return false, nil
	}

	storedHash, err := blockRepo.GetHash(ctx, chainID, blockNumber)
	if err != nil {
		return false, fmt.Errorf("get stored hash for block %d: %w", blockNumber, err)
	}
	if storedHash == (common.Hash{}) {
		return false, nil
	}

	header, err := ethClient.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return false, fmt.Errorf("get chain hash for block %d: %w", blockNumber, err)
	}

	return storedHash != header.Hash(), nil
}

func handleReorg(ctx context.Context, logger *slog.Logger, ethClient eth.Client, s store.Store, chainID int64, reorgBlock uint64) (uint64, error) {
	logger.Warn("reorg detected", "chain_id", chainID, "at_block", reorgBlock)

	ancestor := reorgBlock
	for depth := uint64(0); depth < maxReorgDepth; depth++ {
		if ancestor == 0 {
			break // genesis block is immutable; treat block 0 as a safe ancestor
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

		if storedHash == header.Hash() {
			logger.Info("found common ancestor", "block", ancestor)
			break
		}
	}

	if reorgBlock-ancestor >= maxReorgDepth {
		return 0, fmt.Errorf("reorg deeper than %d blocks — manual intervention required", maxReorgDepth)
	}

	rollbackFrom := ancestor + 1

	err := s.WithTx(ctx, func(txStore store.Store) error {
		if err := txStore.RawEventRepo().DeleteFromBlock(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete raw events: %w", err)
		}
		if err := txStore.AuctionRepo().DeleteFromBlock(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete auctions: %w", err)
		}
		if err := txStore.BlockRepo().DeleteFrom(ctx, chainID, rollbackFrom); err != nil {
			return fmt.Errorf("delete blocks: %w", err)
		}

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
