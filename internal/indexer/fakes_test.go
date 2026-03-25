package indexer

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// --- fakeEthClient ---

type filterLogsCall struct {
	query ethereum.FilterQuery
}

type fakeEthClient struct {
	blockNumber      uint64
	headers          map[uint64]*types.Header
	filterLogsResult []types.Log
	filterLogsCalls  []filterLogsCall
}

func (f *fakeEthClient) BlockNumber(_ context.Context) (uint64, error) {
	return f.blockNumber, nil
}

func (f *fakeEthClient) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	if f.headers != nil {
		if h, ok := f.headers[number.Uint64()]; ok {
			return h, nil
		}
	}
	return makeHeader(number.Uint64()), nil
}

func (f *fakeEthClient) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	f.filterLogsCalls = append(f.filterLogsCalls, filterLogsCall{query: q})
	return f.filterLogsResult, nil
}

func (f *fakeEthClient) Close() {}

// --- fakeStore ---

type fakeStore struct {
	cursor   *fakeCursorRepo
	block    *fakeBlockRepo
	auction  *fakeAuctionRepo
	rawEvent *fakeRawEventRepo
	inTx     bool
}

func newFakeStore() *fakeStore {
	s := &fakeStore{
		auction:  &fakeAuctionRepo{},
		rawEvent: &fakeRawEventRepo{},
	}
	s.cursor = &fakeCursorRepo{store: s}
	s.block = &fakeBlockRepo{store: s}
	return s
}

func (f *fakeStore) AuctionRepo() store.AuctionRepository  { return f.auction }
func (f *fakeStore) RawEventRepo() store.RawEventRepository { return f.rawEvent }
func (f *fakeStore) CursorRepo() store.CursorRepository     { return f.cursor }
func (f *fakeStore) BlockRepo() store.BlockRepository        { return f.block }

func (f *fakeStore) WithTx(_ context.Context, fn func(store.Store) error) error {
	f.inTx = true
	defer func() { f.inTx = false }()
	return fn(f)
}

// --- fakeCursorRepo ---

type cursorUpsertCall struct {
	chainID     int64
	blockNumber uint64
	blockHash   string
	inTx        bool
}

type fakeCursorRepo struct {
	store       *fakeStore
	blockNumber uint64
	blockHash   string
	upsertCalls []cursorUpsertCall
}

func (f *fakeCursorRepo) Get(_ context.Context, _ int64) (uint64, string, error) {
	return f.blockNumber, f.blockHash, nil
}

func (f *fakeCursorRepo) Upsert(_ context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	f.upsertCalls = append(f.upsertCalls, cursorUpsertCall{
		chainID:     chainID,
		blockNumber: blockNumber,
		blockHash:   blockHash,
		inTx:        f.store.inTx,
	})
	return nil
}

// --- fakeBlockRepo ---

type blockInsertCall struct {
	chainID     int64
	blockNumber uint64
	blockHash   string
	parentHash  string
	inTx        bool
}

type fakeBlockRepo struct {
	store       *fakeStore
	insertCalls []blockInsertCall
}

func (f *fakeBlockRepo) Insert(_ context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	f.insertCalls = append(f.insertCalls, blockInsertCall{
		chainID:     chainID,
		blockNumber: blockNumber,
		blockHash:   blockHash,
		parentHash:  parentHash,
		inTx:        f.store.inTx,
	})
	return nil
}

func (f *fakeBlockRepo) GetHash(_ context.Context, _ int64, _ uint64) (string, error) {
	return "", nil
}

// --- fakeAuctionRepo ---

type fakeAuctionRepo struct {
	insertCalls []*cca.Auction
}

func (f *fakeAuctionRepo) Insert(_ context.Context, a *cca.Auction) error {
	f.insertCalls = append(f.insertCalls, a)
	return nil
}

// --- fakeRawEventRepo ---

type fakeRawEventRepo struct {
	insertCalls []*cca.RawEvent
}

func (f *fakeRawEventRepo) Insert(_ context.Context, e *cca.RawEvent) error {
	f.insertCalls = append(f.insertCalls, e)
	return nil
}

// --- fakeEventHandler ---

type fakeEventHandler struct {
	eventName   string
	eventID     common.Hash
	handleCalls []types.Log
}

func (f *fakeEventHandler) EventName() string    { return f.eventName }
func (f *fakeEventHandler) EventID() common.Hash { return f.eventID }

func (f *fakeEventHandler) Handle(_ context.Context, _ int64, log types.Log, _ store.Store) error {
	f.handleCalls = append(f.handleCalls, log)
	return nil
}

// --- helpers ---

func makeHeader(n uint64) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(n),
		ParentHash: common.BigToHash(new(big.Int).SetUint64(n - 1)),
	}
}
