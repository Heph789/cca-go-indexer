package store

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// Store provides access to all domain repositories and transactional writes.
type Store interface {
	AuctionRepo() AuctionRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository
	WithTx(ctx context.Context, fn func(txStore Store) error) error
	Close()
}

// AuctionRepository persists and queries CCA auction records.
type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

// RawEventRepository persists raw on-chain event data for auditing and replay.
type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// CursorRepository tracks the last indexed block per chain for resumable polling.
type CursorRepository interface {
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash string, err error)
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error
}

// BlockRepository stores block hash/parent-hash pairs for reorg detection.
type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error)
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}
