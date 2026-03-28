package store

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type Store interface {
	AuctionRepo() AuctionRepository
	RawEventRepo() RawEventRepository
	CursorRepo() CursorRepository
	BlockRepo() BlockRepository
	Ping(ctx context.Context) error
	WithTx(ctx context.Context, fn func(txStore Store) error) error
	Close()
}

type AuctionRepository interface {
	Insert(ctx context.Context, auction *cca.Auction) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
	GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

type RawEventRepository interface {
	Insert(ctx context.Context, event *cca.RawEvent) error
	DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

type CursorRepository interface {
	Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash common.Hash, err error)
	Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error
}

type BlockRepository interface {
	Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error
	GetHash(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error)
	DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}
