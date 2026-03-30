package handlers

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

type mockStore struct {
	auctionRepo    *mockAuctionRepo
	bidRepo        *mockBidRepo
	checkpointRepo *mockCheckpointRepo
	rawEventRepo   *mockRawEventRepo
	cursorRepo     *mockCursorRepo
	blockRepo      *mockBlockRepo
}

func newMockStore() *mockStore {
	return &mockStore{
		auctionRepo:    &mockAuctionRepo{},
		bidRepo:        &mockBidRepo{},
		checkpointRepo: &mockCheckpointRepo{},
		rawEventRepo:   &mockRawEventRepo{},
		cursorRepo:     &mockCursorRepo{},
		blockRepo:      &mockBlockRepo{},
	}
}

func (m *mockStore) AuctionRepo() store.AuctionRepository             { return m.auctionRepo }
func (m *mockStore) BidRepo() store.BidRepository                     { return m.bidRepo }
func (m *mockStore) CheckpointRepo() store.CheckpointRepository       { return m.checkpointRepo }
func (m *mockStore) RawEventRepo() store.RawEventRepository           { return m.rawEventRepo }
func (m *mockStore) CursorRepo() store.CursorRepository               { return m.cursorRepo }
func (m *mockStore) BlockRepo() store.BlockRepository                 { return m.blockRepo }
func (m *mockStore) WatchedContractRepo() store.WatchedContractRepository { return nil }
func (m *mockStore) RollbackFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}
func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}
func (m *mockStore) Ping(_ context.Context) error { return nil }
func (m *mockStore) Close()                       {}

type mockAuctionRepo struct {
	InsertFn          func(ctx context.Context, auction *cca.Auction) error
	InsertedAuction   *cca.Auction
	DeleteFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
	GetByAddressFn    func(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

func (m *mockAuctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	m.InsertedAuction = auction
	if m.InsertFn != nil {
		return m.InsertFn(ctx, auction)
	}
	return nil
}

func (m *mockAuctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.DeleteFromBlockFn != nil {
		return m.DeleteFromBlockFn(ctx, chainID, fromBlock)
	}
	return nil
}

func (m *mockAuctionRepo) GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
	if m.GetByAddressFn != nil {
		return m.GetByAddressFn(ctx, chainID, auctionAddress)
	}
	return nil, nil
}

type mockRawEventRepo struct {
	InsertFn          func(ctx context.Context, event *cca.RawEvent) error
	InsertedEvent     *cca.RawEvent
	DeleteFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockRawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
	m.InsertedEvent = event
	if m.InsertFn != nil {
		return m.InsertFn(ctx, event)
	}
	return nil
}

func (m *mockRawEventRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.DeleteFromBlockFn != nil {
		return m.DeleteFromBlockFn(ctx, chainID, fromBlock)
	}
	return nil
}

type mockCursorRepo struct{}

func (m *mockCursorRepo) Get(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
	return 0, common.Hash{}, nil
}
func (m *mockCursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
	return nil
}

type mockBlockRepo struct{}

func (m *mockBlockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
	return nil
}
func (m *mockBlockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error) {
	return common.Hash{}, nil
}
func (m *mockBlockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	return nil
}

type mockBidRepo struct {
	InsertFn          func(ctx context.Context, bid *cca.Bid) error
	InsertedBid       *cca.Bid
	DeleteFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockBidRepo) Insert(ctx context.Context, bid *cca.Bid) error {
	m.InsertedBid = bid
	if m.InsertFn != nil {
		return m.InsertFn(ctx, bid)
	}
	return nil
}

func (m *mockBidRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.DeleteFromBlockFn != nil {
		return m.DeleteFromBlockFn(ctx, chainID, fromBlock)
	}
	return nil
}

func (m *mockBidRepo) ListByAuction(_ context.Context, _ int64, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}

func (m *mockBidRepo) ListByAuctionAndOwner(_ context.Context, _ int64, _ string, _ string, _ store.PaginationParams) ([]*cca.Bid, error) {
	return nil, nil
}

func (m *mockBidRepo) GetPrevTickPrice(_ context.Context, _ int64, _ string, _ string) (string, error) {
	return "", nil
}

type mockCheckpointRepo struct {
	InsertFn             func(ctx context.Context, checkpoint *cca.Checkpoint) error
	InsertedCheckpoint   *cca.Checkpoint
	DeleteFromBlockFn    func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockCheckpointRepo) Insert(ctx context.Context, checkpoint *cca.Checkpoint) error {
	m.InsertedCheckpoint = checkpoint
	if m.InsertFn != nil {
		return m.InsertFn(ctx, checkpoint)
	}
	return nil
}

func (m *mockCheckpointRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.DeleteFromBlockFn != nil {
		return m.DeleteFromBlockFn(ctx, chainID, fromBlock)
	}
	return nil
}

func (m *mockCheckpointRepo) GetLatest(_ context.Context, _ int64, _ string) (*cca.Checkpoint, error) {
	return nil, nil
}

func (m *mockCheckpointRepo) ListByAuction(_ context.Context, _ int64, _ string, _ store.PaginationParams) ([]*cca.Checkpoint, error) {
	return nil, nil
}
