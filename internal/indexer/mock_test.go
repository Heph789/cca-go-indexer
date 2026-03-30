package indexer

import (
	"context"
	"math/big"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// --- mockEthClient ---

type mockEthClient struct {
	BlockNumberFn    func(ctx context.Context) (uint64, error)
	HeaderByNumberFn func(ctx context.Context, number *big.Int) (*types.Header, error)
	FilterLogsFn     func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	CloseFn          func()
}

func (m *mockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	if m.BlockNumberFn != nil {
		return m.BlockNumberFn(ctx)
	}
	return 0, nil
}

func (m *mockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if m.HeaderByNumberFn != nil {
		return m.HeaderByNumberFn(ctx, number)
	}
	return &types.Header{}, nil
}

func (m *mockEthClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if m.FilterLogsFn != nil {
		return m.FilterLogsFn(ctx, q)
	}
	return nil, nil
}

func (m *mockEthClient) Close() {
	if m.CloseFn != nil {
		m.CloseFn()
	}
}

// --- mockStore ---

type mockStore struct {
	auctionRepo         *mockAuctionRepo
	rawEventRepo        *mockRawEventRepo
	cursorRepo          *mockCursorRepo
	blockRepo           *mockBlockRepo
	watchedContractRepo *mockWatchedContractRepo
	WithTxFn            func(ctx context.Context, fn func(txStore store.Store) error) error
	RollbackFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
	CloseFn             func()
}

func newMockStore() *mockStore {
	return &mockStore{
		auctionRepo:         &mockAuctionRepo{},
		rawEventRepo:        &mockRawEventRepo{},
		cursorRepo:          &mockCursorRepo{},
		blockRepo:           &mockBlockRepo{},
		watchedContractRepo: &mockWatchedContractRepo{},
	}
}

func (m *mockStore) AuctionRepo() store.AuctionRepository             { return m.auctionRepo }
func (m *mockStore) RawEventRepo() store.RawEventRepository           { return m.rawEventRepo }
func (m *mockStore) CursorRepo() store.CursorRepository               { return m.cursorRepo }
func (m *mockStore) BlockRepo() store.BlockRepository                 { return m.blockRepo }
func (m *mockStore) WatchedContractRepo() store.WatchedContractRepository {
	return m.watchedContractRepo
}

func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	if m.WithTxFn != nil {
		return m.WithTxFn(ctx, fn)
	}
	txStore := &mockStore{
		auctionRepo:         m.auctionRepo,
		rawEventRepo:        m.rawEventRepo,
		cursorRepo:          m.cursorRepo,
		blockRepo:           m.blockRepo,
		watchedContractRepo: m.watchedContractRepo,
	}
	return fn(txStore)
}

func (m *mockStore) RollbackFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.RollbackFromBlockFn != nil {
		return m.RollbackFromBlockFn(ctx, chainID, fromBlock)
	}
	if err := m.rawEventRepo.DeleteFromBlock(ctx, chainID, fromBlock); err != nil {
		return err
	}
	if err := m.auctionRepo.DeleteFromBlock(ctx, chainID, fromBlock); err != nil {
		return err
	}
	if err := m.watchedContractRepo.RollbackCursors(ctx, chainID, fromBlock); err != nil {
		return err
	}
	if err := m.blockRepo.DeleteFrom(ctx, chainID, fromBlock); err != nil {
		return err
	}
	return nil
}

func (m *mockStore) Ping(_ context.Context) error { return nil }

func (m *mockStore) Close() {
	if m.CloseFn != nil {
		m.CloseFn()
	}
}

// --- mockAuctionRepo ---

type mockAuctionRepo struct {
	InsertFn          func(ctx context.Context, auction *cca.Auction) error
	DeleteFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
	GetByAddressFn    func(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error)
}

func (m *mockAuctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
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

// --- mockRawEventRepo ---

type mockRawEventRepo struct {
	InsertFn          func(ctx context.Context, event *cca.RawEvent) error
	DeleteFromBlockFn func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockRawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
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

// --- mockCursorRepo ---

type mockCursorRepo struct {
	GetFn    func(ctx context.Context, chainID int64) (uint64, common.Hash, error)
	UpsertFn func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error
}

func (m *mockCursorRepo) Get(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
	if m.GetFn != nil {
		return m.GetFn(ctx, chainID)
	}
	return 0, common.Hash{}, nil
}

func (m *mockCursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
	if m.UpsertFn != nil {
		return m.UpsertFn(ctx, chainID, blockNumber, blockHash)
	}
	return nil
}

// --- mockBlockRepo ---

type mockBlockRepo struct {
	InsertFn     func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error
	GetHashFn    func(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error)
	DeleteFromFn func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockBlockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
	if m.InsertFn != nil {
		return m.InsertFn(ctx, chainID, blockNumber, blockHash, parentHash)
	}
	return nil
}

func (m *mockBlockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error) {
	if m.GetHashFn != nil {
		return m.GetHashFn(ctx, chainID, blockNumber)
	}
	return common.Hash{}, nil
}

func (m *mockBlockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.DeleteFromFn != nil {
		return m.DeleteFromFn(ctx, chainID, fromBlock)
	}
	return nil
}

// --- mockWatchedContractRepo ---

type mockWatchedContractRepo struct {
	InsertFn                 func(ctx context.Context, contract *cca.WatchedContract) error
	ListCaughtUpFn           func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error)
	ListNeedingBackfillFn    func(ctx context.Context, chainID int64, globalCursor uint64) ([]*cca.WatchedContract, error)
	UpdateLastIndexedBlockFn func(ctx context.Context, chainID int64, address string, lastIndexedBlock uint64) error
	RollbackCursorsFn        func(ctx context.Context, chainID int64, fromBlock uint64) error
}

func (m *mockWatchedContractRepo) Insert(ctx context.Context, contract *cca.WatchedContract) error {
	if m.InsertFn != nil {
		return m.InsertFn(ctx, contract)
	}
	return nil
}

func (m *mockWatchedContractRepo) ListCaughtUp(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
	if m.ListCaughtUpFn != nil {
		return m.ListCaughtUpFn(ctx, chainID, globalCursor)
	}
	return nil, nil
}

func (m *mockWatchedContractRepo) ListNeedingBackfill(ctx context.Context, chainID int64, globalCursor uint64) ([]*cca.WatchedContract, error) {
	if m.ListNeedingBackfillFn != nil {
		return m.ListNeedingBackfillFn(ctx, chainID, globalCursor)
	}
	return nil, nil
}

func (m *mockWatchedContractRepo) UpdateLastIndexedBlock(ctx context.Context, chainID int64, address string, lastIndexedBlock uint64) error {
	if m.UpdateLastIndexedBlockFn != nil {
		return m.UpdateLastIndexedBlockFn(ctx, chainID, address, lastIndexedBlock)
	}
	return nil
}

func (m *mockWatchedContractRepo) RollbackCursors(ctx context.Context, chainID int64, fromBlock uint64) error {
	if m.RollbackCursorsFn != nil {
		return m.RollbackCursorsFn(ctx, chainID, fromBlock)
	}
	return nil
}

// --- mockHandler ---

type mockHandler struct {
	eventName string
	eventID   common.Hash
	HandleFn  func(ctx context.Context, chainID int64, log types.Log, s store.Store) error
	calls     []types.Log
}

func (m *mockHandler) EventName() string    { return m.eventName }
func (m *mockHandler) EventID() common.Hash { return m.eventID }

func (m *mockHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	m.calls = append(m.calls, log)
	if m.HandleFn != nil {
		return m.HandleFn(ctx, chainID, log, s)
	}
	return nil
}

// --- mockBatchHandler ---
// mockBatchHandler implements both EventHandler and BatchEventHandler so tests
// can verify that HandleLogs dispatches to the batch path when available.

type mockBatchHandler struct {
	eventName    string
	eventID      common.Hash
	HandleFn     func(ctx context.Context, chainID int64, log types.Log, s store.Store) error
	HandleLogsFn func(ctx context.Context, chainID int64, logs []types.Log, s store.Store) error
	// calls tracks individual Handle invocations (single-log fallback).
	calls []types.Log
	// batchCalls tracks HandleLogs invocations (batch path).
	batchCalls [][]types.Log
}

func (m *mockBatchHandler) EventName() string    { return m.eventName }
func (m *mockBatchHandler) EventID() common.Hash { return m.eventID }

func (m *mockBatchHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	m.calls = append(m.calls, log)
	if m.HandleFn != nil {
		return m.HandleFn(ctx, chainID, log, s)
	}
	return nil
}

func (m *mockBatchHandler) HandleLogs(ctx context.Context, chainID int64, logs []types.Log, s store.Store) error {
	m.batchCalls = append(m.batchCalls, logs)
	if m.HandleLogsFn != nil {
		return m.HandleLogsFn(ctx, chainID, logs, s)
	}
	return nil
}
