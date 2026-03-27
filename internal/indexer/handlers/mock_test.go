package handlers

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

type mockStore struct {
	auctionRepo  *mockAuctionRepo
	rawEventRepo *mockRawEventRepo
	cursorRepo   *mockCursorRepo
	blockRepo    *mockBlockRepo
}

func newMockStore() *mockStore {
	return &mockStore{
		auctionRepo:  &mockAuctionRepo{},
		rawEventRepo: &mockRawEventRepo{},
		cursorRepo:   &mockCursorRepo{},
		blockRepo:    &mockBlockRepo{},
	}
}

func (m *mockStore) AuctionRepo() store.AuctionRepository   { return m.auctionRepo }
func (m *mockStore) RawEventRepo() store.RawEventRepository  { return m.rawEventRepo }
func (m *mockStore) CursorRepo() store.CursorRepository      { return m.cursorRepo }
func (m *mockStore) BlockRepo() store.BlockRepository        { return m.blockRepo }
func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}
func (m *mockStore) Close() {}

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

func (m *mockCursorRepo) Get(ctx context.Context, chainID int64) (uint64, string, error) {
	return 0, "", nil
}
func (m *mockCursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	return nil
}

type mockBlockRepo struct{}

func (m *mockBlockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	return nil
}
func (m *mockBlockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error) {
	return "", nil
}
func (m *mockBlockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	return nil
}
