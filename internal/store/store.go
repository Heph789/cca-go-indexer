// Package store defines the data access interfaces for the indexer.
package store

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// Store is the top-level data access interface.
// All write operations during block processing happen inside a WithTx
// callback to ensure atomicity.
type Store interface {
	AuctionRepo() AuctionRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository
	WithTx(ctx context.Context, fn func(txStore Store) error) error
	Close()
}

// AuctionRepository handles persistence of decoded AuctionCreated events.
type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// RawEventRepository handles persistence of raw log data.
type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// CursorRepository tracks per-chain indexing progress.
type CursorRepository interface {
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash string, err error)
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error
}

// BlockRepository stores per-block hashes for reorg detection.
type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error)
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}
