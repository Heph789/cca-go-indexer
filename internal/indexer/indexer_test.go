package indexer

import (
	"context"
	"io"
	"log/slog"
	"math/big"
	"sync"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// helper to build a ChainIndexer with common defaults
func setupIndexer(ethClient *mockEthClient, s *mockStore, registry *HandlerRegistry, cfg IndexerConfig) *ChainIndexer {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Millisecond
	}
	if cfg.BlockBatchSize == 0 {
		cfg.BlockBatchSize = 10
	}
	return New(ethClient, s, registry, cfg, noopLogger())
}

// Verifies that Run resumes from the cursor position stored in the database.
func TestIndexer_LoadsCursorFromStore(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Cursor at block 100
	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	// Header for safe head block hash
	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel() // stop after first batch
		return nil, nil
	}

	// CursorRepo.Upsert to avoid nil panic
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
	})

	_ = idx.Run(ctx)

	if capturedQuery.FromBlock == nil {
		t.Fatal("expected FromBlock to be set")
	}
	if capturedQuery.FromBlock.Uint64() != 101 {
		t.Errorf("expected FromBlock=101, got %d", capturedQuery.FromBlock.Uint64())
	}
}

// Verifies that Run begins from StartBlock when no cursor exists.
func TestIndexer_UsesStartBlockWhenNoCursor(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// No cursor
	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 0, "", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 50,
	})

	_ = idx.Run(ctx)

	if capturedQuery.FromBlock == nil {
		t.Fatal("expected FromBlock to be set")
	}
	if capturedQuery.FromBlock.Uint64() != 50 {
		t.Errorf("expected FromBlock=50, got %d", capturedQuery.FromBlock.Uint64())
	}
}

// Verifies that the filter query spans [cursor+1, cursor+batchSize].
func TestIndexer_CorrectBlockRange(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Confirmations:  0,
	})

	_ = idx.Run(ctx)

	if capturedQuery.FromBlock == nil || capturedQuery.ToBlock == nil {
		t.Fatal("expected FromBlock and ToBlock to be set")
	}
	if capturedQuery.FromBlock.Uint64() != 101 {
		t.Errorf("expected FromBlock=101, got %d", capturedQuery.FromBlock.Uint64())
	}
	if capturedQuery.ToBlock.Uint64() != 110 {
		t.Errorf("expected ToBlock=110, got %d", capturedQuery.ToBlock.Uint64())
	}
}

// Verifies that fetched logs are dispatched through the HandlerRegistry.
func TestIndexer_DispatchesLogsThroughRegistry(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	eventID := common.HexToHash("0xaaaa")
	handler := &mockHandler{eventName: "TestEvent", eventID: eventID}

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	testLog := types.Log{
		Topics:      []common.Hash{eventID},
		BlockNumber: 101,
	}

	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return []types.Log{testLog}, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		cancel()
		return nil
	}

	registry := NewRegistry(handler)
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
	})

	_ = idx.Run(ctx)

	if len(handler.calls) == 0 {
		t.Fatal("expected handler to be called at least once")
	}
	if handler.calls[0].Topics[0] != eventID {
		t.Error("handler received wrong log")
	}
}

// Verifies that block headers are inserted for every block in the batch.
func TestIndexer_InsertsBlockHashes(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	var mu sync.Mutex
	var insertedBlocks []uint64

	s.blockRepo.InsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
		mu.Lock()
		insertedBlocks = append(insertedBlocks, blockNumber)
		mu.Unlock()
		return nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		cancel()
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 3,
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(insertedBlocks) < 3 {
		t.Fatalf("expected at least 3 block inserts, got %d", len(insertedBlocks))
	}

	expected := map[uint64]bool{101: true, 102: true, 103: true}
	for _, b := range insertedBlocks[:3] {
		if !expected[b] {
			t.Errorf("unexpected block insert: %d", b)
		}
	}
}

// Verifies that the cursor is upserted to the last block of the batch.
func TestIndexer_AdvancesCursorToEndOfBatch(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	var upsertedBlock uint64
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		upsertedBlock = blockNumber
		cancel()
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
	})

	_ = idx.Run(ctx)

	if upsertedBlock != 110 {
		t.Errorf("expected cursor upserted to 110, got %d", upsertedBlock)
	}
}

// Verifies that Run sleeps instead of fetching logs when cursor is at chain head.
func TestIndexer_SleepsWhenAtChainHead(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	// Chain head is at or below cursor
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 100, nil
	}

	filterLogsCalled := false
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		filterLogsCalled = true
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:      1,
		StartBlock:   1,
		PollInterval: 10 * time.Millisecond,
	})

	_ = idx.Run(ctx)

	if filterLogsCalled {
		t.Error("expected FilterLogs NOT to be called when at chain head")
	}
}

// Verifies that Run sleeps when chain head is below the required confirmations.
func TestIndexer_SleepsWhenChainHeadBelowConfirmations(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 0, "", nil
	}

	// Chain head is 5 but confirmations require 10 — would underflow
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 5, nil
	}

	filterLogsCalled := false
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		filterLogsCalled = true
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:       1,
		StartBlock:    1,
		Confirmations: 10,
		PollInterval:  10 * time.Millisecond,
	})

	_ = idx.Run(ctx)

	if filterLogsCalled {
		t.Error("expected FilterLogs NOT to be called when chainHead < Confirmations")
	}
}

// Verifies that ToBlock is capped at chainHead minus confirmations.
func TestIndexer_AppliesConfirmationsBuffer(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 90, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 110, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		return nil
	}

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 20,
		Confirmations:  10,
	})

	_ = idx.Run(ctx)

	if capturedQuery.ToBlock == nil {
		t.Fatal("expected ToBlock to be set")
	}
	if capturedQuery.ToBlock.Uint64() != 100 {
		t.Errorf("expected ToBlock=100 (safeHead=110-10), got %d", capturedQuery.ToBlock.Uint64())
	}
}

// Verifies that Run returns context.Canceled when the context is already done.
func TestIndexer_StopsWhenContextCancelled(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 0, "", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	registry := NewRegistry()
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
	})

	err := idx.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// Verifies that all store writes (handler, block insert, cursor upsert) occur inside WithTx.
func TestIndexer_AllWritesInsideWithTx(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	eventID := common.HexToHash("0xaaaa")
	handler := &mockHandler{eventName: "TestEvent", eventID: eventID}

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, string, error) {
		return 100, "0xabc", nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	testLog := types.Log{
		Topics:      []common.Hash{eventID},
		BlockNumber: 101,
	}

	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return []types.Log{testLog}, nil
	}

	var withinTx bool
	txBlockRepo := &mockBlockRepo{}
	txCursorRepo := &mockCursorRepo{}
	txStore := &mockStore{
		auctionRepo:  s.auctionRepo,
		rawEventRepo: s.rawEventRepo,
		cursorRepo:   txCursorRepo,
		blockRepo:    txBlockRepo,
	}

	var handlerUsedTxStore bool
	var blockInsertUsedTxStore bool
	var cursorUpsertUsedTxStore bool

	handler.HandleFn = func(ctx context.Context, chainID int64, log types.Log, st store.Store) error {
		if st == txStore {
			handlerUsedTxStore = true
		}
		return nil
	}

	txBlockRepo.InsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
		if withinTx {
			blockInsertUsedTxStore = true
		}
		return nil
	}

	txCursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
		if withinTx {
			cursorUpsertUsedTxStore = true
		}
		cancel()
		return nil
	}

	s.WithTxFn = func(ctx context.Context, fn func(txStore store.Store) error) error {
		withinTx = true
		defer func() { withinTx = false }()
		return fn(txStore)
	}

	registry := NewRegistry(handler)
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 3,
	})

	_ = idx.Run(ctx)

	if !handlerUsedTxStore {
		t.Error("expected handler to receive the tx store")
	}
	if !blockInsertUsedTxStore {
		t.Error("expected BlockRepo.Insert to be called within WithTx")
	}
	if !cursorUpsertUsedTxStore {
		t.Error("expected CursorRepo.Upsert to be called within WithTx")
	}
}
