// Package store defines the data access interfaces for the indexer.
//
// Each interface is intentionally small (1-3 methods) so that implementations
// stay focused and test fakes remain trivial. The top-level Store composes
// them and adds transaction support via WithTx.
package store

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// Store is the top-level data access interface.
//
// All write operations during block processing happen inside a WithTx
// callback to ensure atomicity — if any write fails, everything in that
// tick is rolled back and the cursor is not advanced.
//
// The sub-repositories (AuctionRepo, etc.) returned by a Store created
// inside WithTx share the same underlying database transaction.
type Store interface {
	AuctionRepo() AuctionRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository

	// WithTx runs fn inside a database transaction. The txStore passed to fn
	// shares the transaction, so all repo operations within fn are atomic.
	// If fn returns nil the tx is committed; otherwise it is rolled back.
	WithTx(ctx context.Context, fn func(txStore Store) error) error
}

// AuctionRepository handles persistence of decoded AuctionCreated events.
type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	// DeleteFromBlock removes auctions at or above fromBlock. Used during
	// reorg recovery to discard data from invalidated blocks.
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// RawEventRepository handles persistence of raw log data.
type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	// DeleteFromBlock removes raw events at or above fromBlock (reorg recovery).
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// CursorRepository tracks per-chain indexing progress.
// The cursor records the last fully-processed block so the indexer can
// resume from where it left off after a restart.
type CursorRepository interface {
	// Get returns the saved cursor. Returns (0, "", nil) if no cursor exists
	// (fresh start / first run).
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash string, err error)
	// Upsert creates or updates the cursor for a chain.
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error
}

// BlockRepository stores per-block hashes for reorg detection.
// By recording the hash and parent hash of every processed block, the indexer
// can detect chain reorganizations by comparing stored hashes against the
// chain's current state.
type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error
	// GetHash returns the stored hash for a block, or ("", nil) if not found.
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error)
	// DeleteFrom removes blocks at or above fromBlock (reorg recovery).
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}
