package store

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// Store provides access to all domain repositories and transactional writes.
type Store interface {
	AuctionRepo() AuctionRepository
	BidRepo() BidRepository
	CheckpointRepo() CheckpointRepository
	WatchedContractRepo() WatchedContractRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository
	// RollbackFromBlock deletes all indexed data (events, auctions, blocks,
	// watched contract cursors) at or after fromBlock. Used during reorg recovery.
	RollbackFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	Ping(ctx context.Context) error
	WithTx(ctx context.Context, fn func(txStore Store) error) error
	Close()
}

// AuctionRepository persists and queries CCA auction records.
type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
	List(ctx context.Context, chainID int64, params PaginationParams) ([]*cca.Auction, error)
}

// WatchedContractRepository manages the set of contracts being indexed.
type WatchedContractRepository interface {
	// Insert adds a new watched contract record.
	Insert(ctx context.Context, contract *cca.WatchedContract) error
	// ListCaughtUp returns addresses of contracts whose last_indexed_block >= globalCursor,
	// meaning they are caught up to the global indexer position and should be
	// included in forward polling.
	ListCaughtUp(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error)
	// ListNeedingBackfill returns contracts whose last_indexed_block < globalCursor,
	// meaning they need to be backfilled before joining forward polling.
	ListNeedingBackfill(ctx context.Context, chainID int64, globalCursor uint64) ([]*cca.WatchedContract, error)
	// UpdateLastIndexedBlock advances the per-contract cursor to lastIndexedBlock.
	UpdateLastIndexedBlock(ctx context.Context, chainID int64, address common.Address, lastIndexedBlock uint64) error
	// RollbackCursors sets last_indexed_block = fromBlock - 1 for any contract
	// whose cursor is >= fromBlock, used during reorg recovery.
	RollbackCursors(ctx context.Context, chainID int64, fromBlock uint64) error
}

// RawEventRepository persists raw on-chain event data for auditing and replay.
type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

// CursorRepository tracks the last indexed block per chain for resumable polling.
type CursorRepository interface {
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash common.Hash, err error)
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error
}

// BlockRepository stores block hash/parent-hash pairs for reorg detection.
type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error)
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}

// PaginationParams controls cursor-based pagination for list queries.
type PaginationParams struct {
	Limit             int
	CursorBlockNumber *uint64
	CursorLogIndex    *uint
}

// BidRepository persists and queries CCA bid records.
type BidRepository interface {
	Insert(ctx context.Context, bid *cca.Bid) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	ListByAuction(ctx context.Context, chainID int64, auctionAddress string, params PaginationParams) ([]*cca.Bid, error)
	ListByAuctionAndOwner(ctx context.Context, chainID int64, auctionAddress string, owner string, params PaginationParams) ([]*cca.Bid, error)
	GetPrevTickPrice(ctx context.Context, chainID int64, auctionAddress string, maxPrice string) (string, error)
}

// CheckpointRepository persists and queries CCA checkpoint records.
type CheckpointRepository interface {
	Insert(ctx context.Context, checkpoint *cca.Checkpoint) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	GetLatest(ctx context.Context, chainID int64, auctionAddress string) (*cca.Checkpoint, error)
	ListByAuction(ctx context.Context, chainID int64, auctionAddress string, params PaginationParams) ([]*cca.Checkpoint, error)
}
