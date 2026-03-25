// Package store defines the data access interfaces for the indexer.
// The Store is the top-level entry point; it provides access to
// per-entity repositories and supports atomic transactions via WithTx.
package store

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// Store is the top-level data access interface.
// All write operations during block processing happen inside a WithTx
// callback to ensure atomicity — no partial blocks, no cursor ahead of data.
type Store interface {
	AuctionRepo() AuctionRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository

	// WithTx executes fn inside a single database transaction.
	// All repository operations obtained from the txStore parameter
	// share the same underlying transaction. If fn returns an error,
	// the entire transaction is rolled back.
	WithTx(ctx context.Context, fn func(txStore Store) error) error

	// Close releases database connection pool resources.
	Close()
}

// AuctionRepository handles persistence of decoded AuctionCreated events.
type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	// DeleteFromBlock removes all auctions at or after fromBlock.
	// Used during reorg rollback.
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error

	// --- Read methods (used by the API layer) ---

	// GetByAddress returns a single auction by its on-chain address.
	// Returns (nil, nil) if no auction exists with that address.
	GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

// RawEventRepository handles persistence of raw log data.
// Every event is stored here regardless of type, for auditing and replay.
type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	// DeleteFromBlock removes all raw events at or after fromBlock.
	// Used during reorg rollback.
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// CursorRepository tracks per-chain indexing progress.
// The cursor is the last fully-processed block number + hash.
type CursorRepository interface {
	// Get returns the current cursor position for a chain.
	// Returns (0, "", nil) if no cursor exists yet.
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash string, err error)
	// Upsert creates or updates the cursor for a chain.
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error
}

// BlockRepository stores per-block hashes for reorg detection.
// Before processing new blocks, the indexer compares stored hashes
// against the chain to detect reorganizations.
type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error
	// GetHash returns the stored hash for a specific block, used in reorg detection.
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error)
	// DeleteFrom removes all block records at or after fromBlock.
	// Used during reorg rollback.
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}

// WatchedContractRepository manages the set of contract addresses
// the indexer fetches logs for. Addresses can be added statically
// (config/migration) or dynamically (e.g., a factory handler adding
// a newly-created auction address within the same transaction).
type WatchedContractRepository interface {
	// GetAddresses returns all watched addresses for a chain.
	GetAddresses(ctx context.Context, chainID int64) ([]string, error)
	// Insert adds a new address to the watch list.
	Insert(ctx context.Context, chainID int64, address string, label string) error
}
