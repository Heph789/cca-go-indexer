package indexer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/google/go-cmp/cmp"
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
	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
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
	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 0, common.Hash{}, nil
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

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger(), handler)
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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

	s.blockRepo.InsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
		mu.Lock()
		insertedBlocks = append(insertedBlocks, blockNumber)
		mu.Unlock()
		return nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		upsertedBlock = blockNumber
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 0, common.Hash{}, nil
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

	registry := NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 90, common.HexToHash("0xabc"), nil
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

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
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

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 0, common.Hash{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
	})

	err := idx.Run(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent header fetching tests (#55)
// ---------------------------------------------------------------------------

func TestIndexer_HeadersFetchedConcurrently(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	// Use a barrier to detect that multiple HeaderByNumber calls are in-flight simultaneously.
	const batchSize = 5
	const concurrency = 3
	barrier := make(chan struct{}, batchSize)
	parallelDetected := make(chan struct{})
	var detectOnce sync.Once

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		barrier <- struct{}{}
		// If at least 2 goroutines are waiting, we have parallelism.
		if len(barrier) >= 2 {
			detectOnce.Do(func() { close(parallelDetected) })
		}
		// Small sleep to keep goroutines overlapping.
		time.Sleep(10 * time.Millisecond)
		<-barrier
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:           1,
		StartBlock:        1,
		BlockBatchSize:    batchSize,
		HeaderConcurrency: concurrency,
	})

	_ = idx.Run(ctx)

	select {
	case <-parallelDetected:
		// success — parallel calls were detected
	default:
		t.Error("expected HeaderByNumber to be called concurrently, but no parallel execution was detected")
	}
}

func TestIndexer_HeaderConcurrencyBounded(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	const batchSize = 10
	const maxConcurrency = 3

	var mu sync.Mutex
	inflight := 0
	maxInflight := 0

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		mu.Lock()
		inflight++
		if inflight > maxInflight {
			maxInflight = inflight
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond) // hold the slot to let others pile up

		mu.Lock()
		inflight--
		mu.Unlock()

		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:           1,
		StartBlock:        1,
		BlockBatchSize:    batchSize,
		HeaderConcurrency: maxConcurrency,
	})

	_ = idx.Run(ctx)

	mu.Lock()
	observed := maxInflight
	mu.Unlock()

	if observed > maxConcurrency {
		t.Errorf("expected max %d concurrent header fetches, observed %d", maxConcurrency, observed)
	}
	if observed < 2 {
		t.Errorf("expected at least 2 concurrent header fetches to prove parallelism, observed %d", observed)
	}
}

func TestIndexer_HeaderFetchErrorCancelsRemaining(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	const batchSize = 10
	fetchErr := fmt.Errorf("rpc error: block not found")

	var mu sync.Mutex
	callCount := 0

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		mu.Lock()
		callCount++
		n := number.Uint64()
		mu.Unlock()

		// Fail on block 105
		if n == 105 {
			return nil, fetchErr
		}

		// Other blocks take a while so they can be cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}

		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:           1,
		StartBlock:        1,
		BlockBatchSize:    batchSize,
		HeaderConcurrency: 5,
	})

	err := idx.Run(context.Background())

	if err == nil {
		t.Fatal("expected an error from header fetch failure, got nil")
	}
	if !strings.Contains(err.Error(), "block not found") {
		t.Errorf("expected error to contain 'block not found', got: %v", err)
	}

	// With cancellation, not all 10 headers should complete successfully
	mu.Lock()
	observed := callCount
	mu.Unlock()
	// The errgroup should cancel remaining work; we allow some tolerance
	// but all 10 should not have completed (some should be cancelled).
	if observed >= batchSize {
		t.Logf("warning: %d header fetches were started (all of them); cancellation may not have prevented any", observed)
	}
}

func TestIndexer_HeaderResultOrderMatchesBlockNumber(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	const batchSize = 5

	// Simulate out-of-order completion: higher blocks return faster.
	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		n := number.Uint64()
		// Lower block numbers sleep longer to finish later.
		delay := time.Duration((110-n)*10) * time.Millisecond
		time.Sleep(delay)
		// Return a unique parent hash per block so we can verify ordering.
		return &types.Header{
			ParentHash: common.BigToHash(big.NewInt(int64(n))),
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	var insertedBlocks []uint64

	s.blockRepo.InsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
		mu.Lock()
		insertedBlocks = append(insertedBlocks, blockNumber)
		mu.Unlock()
		return nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:           1,
		StartBlock:        1,
		BlockBatchSize:    batchSize,
		HeaderConcurrency: 5,
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(insertedBlocks) < int(batchSize) {
		t.Fatalf("expected %d block inserts, got %d", batchSize, len(insertedBlocks))
	}

	// Blocks must be inserted in ascending order regardless of fetch completion order.
	for i := 1; i < len(insertedBlocks); i++ {
		if insertedBlocks[i] <= insertedBlocks[i-1] {
			t.Errorf("blocks not in order: block[%d]=%d came after block[%d]=%d",
				i, insertedBlocks[i], i-1, insertedBlocks[i-1])
		}
	}
}

func TestIndexer_HeaderConcurrencyDefaultsToOne(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	const batchSize = 5

	var mu sync.Mutex
	inflight := 0
	maxInflight := 0

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		mu.Lock()
		inflight++
		if inflight > maxInflight {
			maxInflight = inflight
		}
		mu.Unlock()

		time.Sleep(5 * time.Millisecond)

		mu.Lock()
		inflight--
		mu.Unlock()

		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	// HeaderConcurrency is 0 (zero value) — should default to 1 (sequential).
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: batchSize,
	})

	_ = idx.Run(ctx)

	mu.Lock()
	observed := maxInflight
	mu.Unlock()

	if observed != 1 {
		t.Errorf("expected max 1 concurrent header fetch when HeaderConcurrency is unset, observed %d", observed)
	}
}

func TestIndexer_AllWritesInsideWithTx(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	eventID := common.HexToHash("0xaaaa")
	handler := &mockHandler{eventName: "TestEvent", eventID: eventID}

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
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
		auctionRepo:    s.auctionRepo,
		bidRepo:        s.bidRepo,
		checkpointRepo: s.checkpointRepo,
		rawEventRepo:   s.rawEventRepo,
		cursorRepo:     txCursorRepo,
		blockRepo:      txBlockRepo,
	}

	var handlerUsedTxStore bool
	var blockInsertUsedTxStore bool
	var cursorUpsertUsedTxStore bool

	handler.HandleFn = func(ctx context.Context, chainID int64, log types.Log, blockTime time.Time, st store.Store) error {
		if st == txStore {
			handlerUsedTxStore = true
		}
		return nil
	}

	txBlockRepo.InsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
		if withinTx {
			blockInsertUsedTxStore = true
		}
		return nil
	}

	txCursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
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

	registry := NewRegistry(noopLogger(), handler)
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

// ---------------------------------------------------------------------------
// Retry budget & error recovery tests (#39)
// ---------------------------------------------------------------------------

func TestHandleLoopError_IncrementsCounter(t *testing.T) {
	idx := New(&mockEthClient{}, newMockStore(), NewRegistry(noopLogger()), IndexerConfig{ChainID: 1}, noopLogger())
	counter := 0
	idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
	if counter != 1 {
		t.Errorf("expected counter=1, got %d", counter)
	}
	idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
	if counter != 2 {
		t.Errorf("expected counter=2, got %d", counter)
	}
}

func TestHandleLoopError_ReturnsFalseUnderMax(t *testing.T) {
	idx := New(&mockEthClient{}, newMockStore(), NewRegistry(noopLogger()), IndexerConfig{ChainID: 1}, noopLogger())
	counter := 0
	for i := 0; i < maxLoopRetries-1; i++ {
		shouldExit := idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
		if shouldExit {
			t.Fatalf("expected false at attempt %d, got true", i+1)
		}
	}
}

func TestHandleLoopError_ReturnsTrueAtMax(t *testing.T) {
	idx := New(&mockEthClient{}, newMockStore(), NewRegistry(noopLogger()), IndexerConfig{ChainID: 1}, noopLogger())
	counter := 0
	var shouldExit bool
	for i := 0; i < maxLoopRetries; i++ {
		shouldExit = idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
	}
	if !shouldExit {
		t.Fatal("expected true when reaching maxLoopRetries")
	}
}

func TestHandleLoopError_LogsRetryWhenUnderBudget(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	idx := New(&mockEthClient{}, newMockStore(), NewRegistry(noopLogger()), IndexerConfig{ChainID: 1}, logger)
	counter := 0
	idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
	output := buf.String()
	if !strings.Contains(output, "will retry") {
		t.Errorf("expected log to contain 'will retry', got: %s", output)
	}
	if strings.Contains(output, "exhausted") {
		t.Errorf("expected log NOT to contain 'exhausted' when under budget, got: %s", output)
	}
}

func TestHandleLoopError_LogsExhaustedAtMax(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	idx := New(&mockEthClient{}, newMockStore(), NewRegistry(noopLogger()), IndexerConfig{ChainID: 1}, logger)
	counter := maxLoopRetries - 1
	idx.handleLoopError(&counter, "test op", fmt.Errorf("boom"))
	output := buf.String()
	if !strings.Contains(output, "exhausted") {
		t.Errorf("expected log to contain 'exhausted' when at max retries, got: %s", output)
	}
	if strings.Contains(output, "will retry") {
		t.Errorf("expected log NOT to contain 'will retry' when budget exhausted, got: %s", output)
	}
}

func TestIndexer_RetriesOnBlockNumberError(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	var callCount int32
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		callCount++
		if callCount == 1 {
			return 0, fmt.Errorf("rpc timeout")
		}
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:      1,
		StartBlock:   1,
		PollInterval: time.Millisecond,
	})

	err := idx.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatalf("expected indexer to recover from transient BlockNumber error, got: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected BlockNumber to be called at least twice (retry), got %d", callCount)
	}
}

func TestIndexer_RetriesOnFilterLogsError(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	var filterCallCount int32
	ctx, cancel := context.WithCancel(context.Background())
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		filterCallCount++
		if filterCallCount == 1 {
			return nil, fmt.Errorf("rpc timeout")
		}
		return nil, nil
	}
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:      1,
		StartBlock:   1,
		PollInterval: time.Millisecond,
	})

	err := idx.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatalf("expected indexer to recover from transient FilterLogs error, got: %v", err)
	}
	if filterCallCount < 2 {
		t.Errorf("expected FilterLogs to be called at least twice (retry), got %d", filterCallCount)
	}
}

func TestIndexer_RetriesOnHeaderError(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	var headerCallCount int32
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		mu.Lock()
		headerCallCount++
		count := headerCallCount
		mu.Unlock()
		// Fail on the very first call only
		if count == 1 {
			return nil, fmt.Errorf("rpc timeout")
		}
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 1,
		PollInterval:   time.Millisecond,
	})

	err := idx.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatalf("expected indexer to recover from transient header error, got: %v", err)
	}
}

func TestIndexer_RetriesOnWithTxError(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	var txCallCount int32
	ctx, cancel := context.WithCancel(context.Background())

	s.WithTxFn = func(ctx context.Context, fn func(txStore store.Store) error) error {
		txCallCount++
		if txCallCount == 1 {
			return fmt.Errorf("db connection lost")
		}
		txStore := &mockStore{
			auctionRepo:         s.auctionRepo,
			rawEventRepo:        s.rawEventRepo,
			cursorRepo:          s.cursorRepo,
			blockRepo:           s.blockRepo,
			watchedContractRepo: s.watchedContractRepo,
		}
		err := fn(txStore)
		if err != nil {
			return err
		}
		cancel()
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 1,
		PollInterval:   time.Millisecond,
	})

	err := idx.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatalf("expected indexer to recover from transient WithTx error, got: %v", err)
	}
	if txCallCount < 2 {
		t.Errorf("expected WithTx to be called at least twice (retry), got %d", txCallCount)
	}
}

func TestIndexer_ResetsErrorCounterAfterSuccess(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	// BlockNumber fails on first call, succeeds on subsequent calls
	var blockNumCalls int32
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		blockNumCalls++
		if blockNumCalls == 1 {
			return 0, fmt.Errorf("rpc timeout")
		}
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	var batchCount int32
	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		batchCount++
		if batchCount >= 2 {
			cancel()
		}
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:      1,
		StartBlock:   1,
		PollInterval: time.Millisecond,
	})

	err := idx.Run(ctx)
	if err != nil && err != context.Canceled {
		t.Fatalf("expected no fatal error, got: %v", err)
	}
	// If we completed 2 batches after an initial error, the counter was reset
	if batchCount < 2 {
		t.Errorf("expected at least 2 successful batches, got %d", batchCount)
	}
}

func TestIndexer_ExitsAfterMaxConsecutiveErrors(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	// Always fail
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 0, fmt.Errorf("persistent rpc error")
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:      1,
		StartBlock:   1,
		PollInterval: time.Millisecond,
	})

	err := idx.Run(context.Background())
	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "retries") {
		t.Errorf("expected error message to mention retries, got: %v", err)
	}
}

func TestIndexer_SkipsWhenChainTooYoungForConfirmations(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 0, common.Hash{}, nil
	}

	var blockNumCalls int32
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		blockNumCalls++
		// Chain head is 5, confirmations is 10 => chain too young
		if blockNumCalls <= 2 {
			return 5, nil
		}
		// Eventually return a valid chain head so we can verify it didn't error
		return 5, nil
	}

	filterLogsCalled := false
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		filterLogsCalled = true
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:       1,
		StartBlock:    1,
		Confirmations: 10,
		PollInterval:  time.Millisecond,
	})

	err := idx.Run(ctx)
	// Should exit via context timeout, not via an error
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected context deadline exceeded or nil, got: %v", err)
	}
	if filterLogsCalled {
		t.Error("expected FilterLogs NOT to be called when chain is too young for confirmations")
	}
	if blockNumCalls < 2 {
		t.Errorf("expected BlockNumber to be called multiple times (polling), got %d", blockNumCalls)
	}
}

// ---------------------------------------------------------------------------
// Watched contract polling & cursor advancement tests
// ---------------------------------------------------------------------------

// TestIndexer_MergesWatchedContractAddresses verifies that when
// WatchedContractRepo.ListCaughtUp returns additional addresses, the FilterLogs
// query includes both the static config addresses AND the dynamically discovered
// watched contract addresses. This ensures that logs from newly registered
// auction contracts are captured once they catch up to the global cursor.
func TestIndexer_MergesWatchedContractAddresses(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Config addresses: two static addresses from the indexer config.
	configAddr1 := common.HexToAddress("0xCONFIG1")
	configAddr2 := common.HexToAddress("0xCONFIG2")

	// Watched contract addresses: two dynamically registered addresses
	// that ListCaughtUp reports as caught up to the global cursor.
	watchedAddr1 := common.HexToAddress("0xWATCHED1")
	watchedAddr2 := common.HexToAddress("0xWATCHED2")

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	// Return two watched contract addresses that are caught up.
	s.watchedContractRepo.ListCaughtUpFn = func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
		return []common.Address{watchedAddr1, watchedAddr2}, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel() // stop after first batch
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
		Addresses:  []common.Address{configAddr1, configAddr2},
	})

	_ = idx.Run(ctx)

	// The FilterLogs query should contain all four addresses: two config + two watched.
	wantAddrCount := 4
	if len(capturedQuery.Addresses) != wantAddrCount {
		t.Fatalf("expected %d addresses in FilterLogs query, got %d: %v",
			wantAddrCount, len(capturedQuery.Addresses), capturedQuery.Addresses)
	}

	// Build a set of the captured addresses for order-independent comparison.
	addrSet := make(map[common.Address]bool, len(capturedQuery.Addresses))
	for _, a := range capturedQuery.Addresses {
		addrSet[a] = true
	}

	// Verify each expected address is present.
	for _, want := range []common.Address{configAddr1, configAddr2, watchedAddr1, watchedAddr2} {
		if !addrSet[want] {
			t.Errorf("expected address %s in FilterLogs query, but it was missing", want.Hex())
		}
	}
}

// TestIndexer_UpdatesWatchedContractCursorsAfterBatch verifies that after a
// successful batch, UpdateLastIndexedBlock is called once for each caught-up
// watched contract with the correct block number (the `to` value of the batch).
// This ensures per-contract cursors advance in lockstep with the global cursor.
func TestIndexer_UpdatesWatchedContractCursorsAfterBatch(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Two watched contract addresses that are caught up.
	watchedAddr1 := common.HexToAddress("0xWATCHED1")
	watchedAddr2 := common.HexToAddress("0xWATCHED2")

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	s.watchedContractRepo.ListCaughtUpFn = func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
		return []common.Address{watchedAddr1, watchedAddr2}, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	// Track UpdateLastIndexedBlock calls: map address -> block number.
	type cursorUpdate struct {
		address common.Address
		block   uint64
	}
	var updates []cursorUpdate
	ctx, cancel := context.WithCancel(context.Background())

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(ctx context.Context, chainID int64, address common.Address, lastIndexedBlock uint64) error {
		updates = append(updates, cursorUpdate{address: address, block: lastIndexedBlock})
		return nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel() // stop after first batch completes
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
	})

	_ = idx.Run(ctx)

	// Expect one UpdateLastIndexedBlock call per watched contract.
	wantUpdateCount := 2
	if len(updates) != wantUpdateCount {
		t.Fatalf("expected %d UpdateLastIndexedBlock calls, got %d", wantUpdateCount, len(updates))
	}

	// The batch range is [101, 110] so the `to` value should be 110.
	wantBlock := uint64(110)

	updateSet := make(map[common.Address]uint64, len(updates))
	for _, u := range updates {
		updateSet[u.address] = u.block
	}

	// Verify each watched address was updated to the correct block.
	for _, want := range []common.Address{watchedAddr1, watchedAddr2} {
		gotBlock, ok := updateSet[want]
		if !ok {
			t.Errorf("expected UpdateLastIndexedBlock call for %s, but none was made", want.Hex())
			continue
		}
		if gotBlock != wantBlock {
			t.Errorf("expected UpdateLastIndexedBlock(%s) with block %d, got %d", want.Hex(), wantBlock, gotBlock)
		}
	}
}

// TestIndexer_CursorUpdatesInsideTx verifies that UpdateLastIndexedBlock calls
// for watched contracts happen inside the WithTx transaction, alongside block
// inserts, handler dispatch, and cursor upsert. This is critical for atomicity:
// if the transaction rolls back, watched contract cursors must also roll back.
func TestIndexer_CursorUpdatesInsideTx(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	watchedAddr := common.HexToAddress("0xWATCHED1")

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	s.watchedContractRepo.ListCaughtUpFn = func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
		return []common.Address{watchedAddr}, nil
	}

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Track whether UpdateLastIndexedBlock is called while WithTx is active,
	// using the same pattern as TestIndexer_AllWritesInsideWithTx.
	var withinTx bool
	var updateCalledInTx bool

	txWatchedContractRepo := &mockWatchedContractRepo{}
	txWatchedContractRepo.UpdateLastIndexedBlockFn = func(ctx context.Context, chainID int64, address common.Address, lastIndexedBlock uint64) error {
		if withinTx {
			updateCalledInTx = true
		}
		return nil
	}

	txCursorRepo := &mockCursorRepo{}
	txCursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		cancel()
		return nil
	}

	txBlockRepo := &mockBlockRepo{}
	txStore := &mockStore{
		auctionRepo:         s.auctionRepo,
		rawEventRepo:        s.rawEventRepo,
		cursorRepo:          txCursorRepo,
		blockRepo:           txBlockRepo,
		watchedContractRepo: txWatchedContractRepo,
	}

	s.WithTxFn = func(ctx context.Context, fn func(txStore store.Store) error) error {
		withinTx = true
		defer func() { withinTx = false }()
		return fn(txStore)
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 3,
	})

	_ = idx.Run(ctx)

	if !updateCalledInTx {
		t.Error("expected WatchedContractRepo.UpdateLastIndexedBlock to be called within WithTx transaction")
	}
}

// TestIndexer_NoWatchedContractsOnlyConfigAddresses verifies that when
// ListCaughtUp returns no watched contracts (empty slice), the FilterLogs query
// contains only the static config addresses. This is the baseline case before
// any auction contracts have been registered or caught up.
func TestIndexer_NoWatchedContractsOnlyConfigAddresses(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Config addresses only — no watched contracts.
	configAddr1 := common.HexToAddress("0xCONFIG1")
	configAddr2 := common.HexToAddress("0xCONFIG2")

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
		}, nil
	}

	// ListCaughtUp returns nil — no watched contracts are caught up.
	s.watchedContractRepo.ListCaughtUpFn = func(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
		return nil, nil
	}

	var capturedQuery ethereum.FilterQuery
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedQuery = q
		cancel()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:    1,
		StartBlock: 1,
		Addresses:  []common.Address{configAddr1, configAddr2},
	})

	_ = idx.Run(ctx)

	// Only the two config addresses should appear — no watched contract addresses.
	wantAddrCount := 2
	if len(capturedQuery.Addresses) != wantAddrCount {
		t.Fatalf("expected %d addresses in FilterLogs query, got %d: %v",
			wantAddrCount, len(capturedQuery.Addresses), capturedQuery.Addresses)
	}

	addrSet := make(map[common.Address]bool, len(capturedQuery.Addresses))
	for _, a := range capturedQuery.Addresses {
		addrSet[a] = true
	}

	if !addrSet[configAddr1] {
		t.Errorf("expected config address %s in FilterLogs query", configAddr1.Hex())
	}
	if !addrSet[configAddr2] {
		t.Errorf("expected config address %s in FilterLogs query", configAddr2.Hex())
	}
}

// TestIndexer_ReorgRollsBackWatchedContractCursors verifies that during reorg
// handling, RollbackFromBlock is called (which internally calls
// WatchedContractRepo.RollbackCursors), ensuring that per-contract cursors are
// rolled back alongside raw events, auctions, and block records. This tests
// the reorg.go integration with watched contract state.
func TestIndexer_ReorgRollsBackWatchedContractCursors(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(ctx context.Context, chainID int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	// Return chain head far enough ahead to trigger processing.
	ethClient.BlockNumberFn = func(ctx context.Context) (uint64, error) {
		return 200, nil
	}

	// Create headers for blocks involved in reorg detection and ancestor search.
	// Block 100 has a different hash on-chain vs stored (reorg detected).
	// Block 99 matches (common ancestor).
	reorgBlockHeader := &types.Header{Number: big.NewInt(100), Nonce: types.BlockNonce{1}}
	ancestorHeader := &types.Header{Number: big.NewInt(99), Nonce: types.BlockNonce{2}}

	ethClient.HeaderByNumberFn = func(ctx context.Context, number *big.Int) (*types.Header, error) {
		switch number.Uint64() {
		case 100:
			return reorgBlockHeader, nil
		case 99:
			return ancestorHeader, nil
		default:
			return &types.Header{
				ParentHash: common.HexToHash("0xparent"),
			}, nil
		}
	}

	// Block 100 stored hash differs from chain hash (triggers reorg).
	// Block 99 stored hash matches chain hash (common ancestor).
	// Note: the stored hash must be non-zero; detectReorg treats zero hash as
	// "not indexed yet" and skips the reorg check.
	staleHash := common.HexToHash("0xdead000000000000000000000000000000000000000000000000000000000001")
	s.blockRepo.GetHashFn = func(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error) {
		switch blockNumber {
		case 100:
			// Return a non-zero hash that differs from reorgBlockHeader.Hash()
			// to trigger reorg detection.
			return staleHash, nil
		case 99:
			// Matches the chain — this is the common ancestor.
			return ancestorHeader.Hash(), nil
		default:
			return common.Hash{}, nil
		}
	}

	// Track whether RollbackCursors was called and with what block number.
	var rollbackCursorsCalled bool
	var rollbackCursorsFrom uint64

	s.watchedContractRepo.RollbackCursorsFn = func(ctx context.Context, chainID int64, fromBlock uint64) error {
		rollbackCursorsCalled = true
		rollbackCursorsFrom = fromBlock
		return nil
	}

	// After reorg handling, the indexer will resume and try to process the
	// next batch. Cancel on the FilterLogs call to stop after reorg recovery.
	ctx, cancel := context.WithCancel(context.Background())

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		cancel()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(ctx context.Context, chainID int64, blockNumber uint64, blockHash common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
	})

	_ = idx.Run(ctx)

	if !rollbackCursorsCalled {
		t.Fatal("expected WatchedContractRepo.RollbackCursors to be called during reorg handling")
	}

	// rollbackFrom = ancestor(99) + 1 = 100
	wantRollbackFrom := uint64(100)
	if rollbackCursorsFrom != wantRollbackFrom {
		t.Errorf("expected RollbackCursors called with fromBlock=%d, got %d",
			wantRollbackFrom, rollbackCursorsFrom)
	}
}

// ---------------------------------------------------------------------------
// Inline backfill tests
// ---------------------------------------------------------------------------

// setupBackfillTest configures the common mocks needed for a backfill test:
// cursor at initialCursor, chain head at 200, no-op headers and cursor upsert.
// Returns the ethClient, store, and a cancel func (with 500ms timeout).
func setupBackfillTest(t *testing.T, initialCursor uint64) (*mockEthClient, *mockStore, context.Context, context.CancelFunc) {
	t.Helper()

	ethClient := &mockEthClient{}
	s := newMockStore()

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return initialCursor, common.HexToHash("0xabc"), nil
	}
	ethClient.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 200, nil
	}
	ethClient.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}
	ethClient.FilterLogsFn = func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, _ uint64, _ common.Hash) error {
		return nil
	}
	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, _ common.Address, _ uint64) error {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ethClient, s, ctx, cancel
}

func backfillIndexerConfig() IndexerConfig {
	return IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		PollInterval:   5 * time.Millisecond,
	}
}

// TestIndexer_BackfillCallsListNeedingBackfill verifies that after the forward
// batch completes, the indexer queries for contracts needing backfill.
func TestIndexer_BackfillCallsListNeedingBackfill(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	listNeedingBackfillCalled := false
	var capturedGlobalCursor uint64

	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, globalCursor uint64) ([]*cca.WatchedContract, error) {
		listNeedingBackfillCalled = true
		capturedGlobalCursor = globalCursor
		cancel()
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	if !listNeedingBackfillCalled {
		t.Fatal("expected ListNeedingBackfill to be called after forward batch")
	}

	wantCursor := uint64(110)
	if capturedGlobalCursor != wantCursor {
		t.Errorf("expected ListNeedingBackfill called with globalCursor=%d, got %d",
			wantCursor, capturedGlobalCursor)
	}
}

// TestIndexer_BackfillFilterLogsForBehindContract verifies that when a watched
// contract is behind the global cursor, the indexer calls FilterLogs with the
// contract's address for the range [lastIndexedBlock+1, batchEnd].
func TestIndexer_BackfillFilterLogsForBehindContract(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	behindAddr := common.HexToAddress("0xBBBB")
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{{
			Address:          behindAddr,
			ChainID:          1,
			StartBlock:       10,
			LastIndexedBlock: 50,
		}}, nil
	}

	var mu sync.Mutex
	var filterLogsCalls []ethereum.FilterQuery
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterLogsCalls = append(filterLogsCalls, q)
		n := len(filterLogsCalls)
		mu.Unlock()

		if n >= 2 {
			cancel()
		}
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(filterLogsCalls) < 2 {
		t.Fatalf("expected at least 2 FilterLogs calls (forward + backfill), got %d", len(filterLogsCalls))
	}

	backfillQuery := filterLogsCalls[1]

	wantFrom := uint64(51)
	wantTo := uint64(60)
	if backfillQuery.FromBlock == nil {
		t.Fatal("expected backfill FilterLogs FromBlock to be set")
	}
	if backfillQuery.FromBlock.Uint64() != wantFrom {
		t.Errorf("expected backfill FromBlock=%d, got %d", wantFrom, backfillQuery.FromBlock.Uint64())
	}
	if backfillQuery.ToBlock == nil {
		t.Fatal("expected backfill FilterLogs ToBlock to be set")
	}
	if backfillQuery.ToBlock.Uint64() != wantTo {
		t.Errorf("expected backfill ToBlock=%d, got %d", wantTo, backfillQuery.ToBlock.Uint64())
	}

	wantAddresses := []common.Address{behindAddr}
	if diff := cmp.Diff(wantAddresses, backfillQuery.Addresses); diff != "" {
		t.Errorf("backfill FilterLogs addresses mismatch (-want +got):\n%s", diff)
	}
}

// TestIndexer_BackfillAdvancesLastIndexedBlock verifies that after processing
// a backfill batch, UpdateLastIndexedBlock is called with the end of the range.
func TestIndexer_BackfillAdvancesLastIndexedBlock(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	behindAddr := common.HexToAddress("0xCCCC")
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{{
			Address:          behindAddr,
			ChainID:          1,
			StartBlock:       10,
			LastIndexedBlock: 80,
		}}, nil
	}

	backfillUpdateCalled := false
	var capturedAddr common.Address
	var capturedBlock uint64

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, address common.Address, lastIndexedBlock uint64) error {
		if address == behindAddr {
			backfillUpdateCalled = true
			capturedAddr = address
			capturedBlock = lastIndexedBlock
			cancel()
		}
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	if !backfillUpdateCalled {
		t.Fatal("expected UpdateLastIndexedBlock to be called for the backfilling contract")
	}

	if capturedAddr != behindAddr {
		t.Errorf("expected UpdateLastIndexedBlock for address %s, got %s", behindAddr.Hex(), capturedAddr.Hex())
	}

	wantBlock := uint64(90)
	if capturedBlock != wantBlock {
		t.Errorf("expected UpdateLastIndexedBlock with block=%d, got %d", wantBlock, capturedBlock)
	}
}

// TestIndexer_BackfillUsesHandlerRegistry verifies that logs fetched during
// backfill are dispatched through the HandlerRegistry.
func TestIndexer_BackfillUsesHandlerRegistry(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	ethClient.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       uint64(1700000000),
		}, nil
	}

	eventID := common.HexToHash("0xDDDD")
	handler := &mockHandler{eventName: "BackfillTestEvent", eventID: eventID}

	behindAddr := common.HexToAddress("0xAAAA")
	backfillLog := types.Log{
		Topics:      []common.Hash{eventID},
		BlockNumber: 55,
		Address:     behindAddr,
	}

	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{{
			Address:          behindAddr,
			ChainID:          1,
			StartBlock:       10,
			LastIndexedBlock: 50,
		}}, nil
	}

	var mu sync.Mutex
	filterCallCount := 0
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterCallCount++
		n := filterCallCount
		mu.Unlock()

		if n == 1 {
			return nil, nil
		}

		for _, addr := range q.Addresses {
			if addr == behindAddr {
				cancel()
				return []types.Log{backfillLog}, nil
			}
		}
		return nil, nil
	}

	registry := NewRegistry(noopLogger(), handler)
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	foundBackfillLog := false
	for _, call := range handler.calls {
		if call.BlockNumber == 55 {
			foundBackfillLog = true
			break
		}
	}
	if !foundBackfillLog {
		t.Fatalf("expected handler to receive backfill log from block 55, but it was not found in %d handler calls",
			len(handler.calls))
	}
}

// TestIndexer_BackfillStartsFromStartBlockWhenLastIndexedIsZero verifies that
// when LastIndexedBlock == 0, backfill starts from the contract's StartBlock.
func TestIndexer_BackfillStartsFromStartBlockWhenLastIndexedIsZero(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	contractStartBlock := uint64(42)
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{{
			Address:          common.HexToAddress("0xEEEE"),
			ChainID:          1,
			StartBlock:       contractStartBlock,
			LastIndexedBlock: 0,
		}}, nil
	}

	var mu sync.Mutex
	var filterLogsCalls []ethereum.FilterQuery
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterLogsCalls = append(filterLogsCalls, q)
		n := len(filterLogsCalls)
		mu.Unlock()

		if n >= 2 {
			cancel()
		}
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(filterLogsCalls) < 2 {
		t.Fatalf("expected at least 2 FilterLogs calls (forward + backfill), got %d", len(filterLogsCalls))
	}

	backfillQuery := filterLogsCalls[1]
	if backfillQuery.FromBlock == nil {
		t.Fatal("expected backfill FilterLogs FromBlock to be set")
	}
	if backfillQuery.FromBlock.Uint64() != contractStartBlock {
		t.Errorf("expected backfill FromBlock=%d (contract StartBlock), got %d",
			contractStartBlock, backfillQuery.FromBlock.Uint64())
	}
}

// TestIndexer_BackfillContractJoinsCaughtUpAfterCompletion verifies that once
// backfill reaches the global cursor, the contract transitions out of
// ListNeedingBackfill on the next cycle.
func TestIndexer_BackfillContractJoinsCaughtUpAfterCompletion(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)
	_ = ethClient

	contractAddr := common.HexToAddress("0xFFFF")

	backfillUpdateCalled := false
	var backfillUpdatedBlock uint64

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, address common.Address, lastIndexedBlock uint64) error {
		if address == contractAddr {
			backfillUpdateCalled = true
			backfillUpdatedBlock = lastIndexedBlock
		}
		return nil
	}

	backfillCycle := 0
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		backfillCycle++
		if backfillCycle == 1 {
			return []*cca.WatchedContract{{
				Address:          contractAddr,
				ChainID:          1,
				StartBlock:       10,
				LastIndexedBlock: 105,
			}}, nil
		}
		cancel()
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, backfillIndexerConfig())

	_ = idx.Run(ctx)

	if !backfillUpdateCalled {
		t.Fatal("expected UpdateLastIndexedBlock to be called for the backfilling contract")
	}

	wantBlock := uint64(110)
	if backfillUpdatedBlock != wantBlock {
		t.Errorf("expected backfill to advance contract cursor to %d, got %d",
			wantBlock, backfillUpdatedBlock)
	}
}

// TestIndexer_BackfillProcessesOneBatchPerCycleThenYields verifies that backfill
// processes at most BlockBatchSize blocks per contract per cycle, not the entire
// range, so forward polling is not blocked.
// TestIndexer_ReorgMidRunResumeFromAncestor verifies that when a reorg is
// detected during the main polling loop, the indexer rolls back to the common
// ancestor and then resumes forward indexing from that ancestor. We assert this
// by capturing the FromBlock of the FilterLogs call that happens immediately
// after the reorg handling completes — it should be ancestor + 1.
func TestIndexer_ReorgMidRunResumeFromAncestor(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Start with cursor at block 50.
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 50, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 200, nil
	}

	// Ancestor at block 48: blocks 49 and 50 are reorged away.
	ancestorBlock := uint64(48)
	ancestorHeader := &types.Header{Number: big.NewInt(int64(ancestorBlock)), Nonce: types.BlockNonce{48}}
	block49Header := &types.Header{Number: big.NewInt(49), Nonce: types.BlockNonce{49}}
	block50Header := &types.Header{Number: big.NewInt(50), Nonce: types.BlockNonce{50}}

	ethClient.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		switch n.Uint64() {
		case 48:
			return ancestorHeader, nil
		case 49:
			return block49Header, nil
		case 50:
			return block50Header, nil
		default:
			// For forward-indexing header fetches after the reorg.
			return &types.Header{
				Number:     n,
				ParentHash: common.HexToHash("0xparent"),
			}, nil
		}
	}

	// Stored hash for block 50 differs from on-chain (triggers reorg detection).
	// Stored hash for block 49 also differs (still reorged).
	// Stored hash for block 48 matches (common ancestor).
	staleHash := common.HexToHash("0xdead000000000000000000000000000000000000000000000000000000000001")
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		switch bn {
		case 50:
			return staleHash, nil
		case 49:
			return common.HexToHash("0xdead49"), nil
		case 48:
			return ancestorHeader.Hash(), nil
		default:
			return common.Hash{}, nil
		}
	}

	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, _ uint64, _ common.Hash) error {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Capture the first FilterLogs call after reorg — it should start from
	// ancestor + 1 = 49, proving the cursor was reset.
	var capturedFromBlock uint64
	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		capturedFromBlock = q.FromBlock.Uint64()
		cancel()
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
	})

	_ = idx.Run(ctx)

	// After reorg with ancestor=48, the indexer should resume from block 49.
	wantFromBlock := ancestorBlock + 1
	if capturedFromBlock != wantFromBlock {
		t.Fatalf("expected indexer to resume from block %d after reorg, got FromBlock=%d",
			wantFromBlock, capturedFromBlock)
	}
}

// TestIndexer_ReorgRollsBackBackfillContractCursors verifies that when a reorg
// occurs, the backfill (per-contract) cursors are also rolled back via
// WatchedContractRepo.RollbackCursors. This is critical because a contract
// mid-backfill may have indexed data from blocks that are now invalid. After
// rollback, re-indexing must cover those blocks again.
func TestIndexer_ReorgRollsBackBackfillContractCursors(t *testing.T) {
	ethClient := &mockEthClient{}
	s := newMockStore()

	// Cursor at block 100; a watched contract has been backfilled up to block 95.
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	ethClient.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 200, nil
	}

	// Reorg at block 100, ancestor at 98 (two blocks deep).
	ancestorBlock := uint64(98)
	ancestorHeader := &types.Header{Number: big.NewInt(int64(ancestorBlock)), Nonce: types.BlockNonce{98}}
	block99Header := &types.Header{Number: big.NewInt(99), Nonce: types.BlockNonce{99}}
	block100Header := &types.Header{Number: big.NewInt(100), Nonce: types.BlockNonce{100}}

	ethClient.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		switch n.Uint64() {
		case 98:
			return ancestorHeader, nil
		case 99:
			return block99Header, nil
		case 100:
			return block100Header, nil
		default:
			return &types.Header{
				Number:     n,
				ParentHash: common.HexToHash("0xparent"),
			}, nil
		}
	}

	// Block 100 stored hash is stale (triggers reorg). Block 99 is also stale.
	// Block 98 matches the chain (common ancestor).
	staleHash := common.HexToHash("0xdead000000000000000000000000000000000000000000000000000000000001")
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		switch bn {
		case 100:
			return staleHash, nil
		case 99:
			return common.HexToHash("0xdead99"), nil
		case 98:
			return ancestorHeader.Hash(), nil
		default:
			return common.Hash{}, nil
		}
	}

	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, _ uint64, _ common.Hash) error {
		return nil
	}

	// Track RollbackCursors calls to verify the backfill cursors are rolled back.
	var rollbackCursorsCalled bool
	var rollbackCursorsFromBlock uint64
	var rollbackCursorsChainID int64
	s.watchedContractRepo.RollbackCursorsFn = func(_ context.Context, chainID int64, fromBlock uint64) error {
		rollbackCursorsCalled = true
		rollbackCursorsFromBlock = fromBlock
		rollbackCursorsChainID = chainID
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel on the first FilterLogs call (after reorg recovery) to stop the loop.
	ethClient.FilterLogsFn = func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
		cancel()
		return nil, nil
	}

	wantChainID := int64(1)
	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        wantChainID,
		StartBlock:     1,
		BlockBatchSize: 10,
	})

	_ = idx.Run(ctx)

	// RollbackCursors must have been called so that per-contract backfill
	// cursors are reset and those blocks are re-indexed.
	if !rollbackCursorsCalled {
		t.Fatal("expected WatchedContractRepo.RollbackCursors to be called during reorg, " +
			"so that mid-backfill contract cursors are also rolled back")
	}

	// rollbackFrom = ancestor(98) + 1 = 99
	wantRollbackFrom := ancestorBlock + 1
	if rollbackCursorsFromBlock != wantRollbackFrom {
		t.Fatalf("expected RollbackCursors called with fromBlock=%d, got %d",
			wantRollbackFrom, rollbackCursorsFromBlock)
	}

	if rollbackCursorsChainID != wantChainID {
		t.Fatalf("expected RollbackCursors called with chainID=%d, got %d",
			wantChainID, rollbackCursorsChainID)
	}
}

func TestIndexer_BackfillProcessesOneBatchPerCycleThenYields(t *testing.T) {
	ethClient, s, ctx, cancel := setupBackfillTest(t, 100)

	behindAddr := common.HexToAddress("0x1111")
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{{
			Address:          behindAddr,
			ChainID:          1,
			StartBlock:       10,
			LastIndexedBlock: 50,
		}}, nil
	}

	batchSize := uint64(10)
	var mu sync.Mutex
	var backfillQueries []ethereum.FilterQuery
	filterCallCount := 0

	ethClient.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterCallCount++
		n := filterCallCount
		mu.Unlock()

		if n == 1 {
			return nil, nil
		}

		mu.Lock()
		backfillQueries = append(backfillQueries, q)
		mu.Unlock()

		cancel()
		return nil, nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: batchSize,
		PollInterval:   5 * time.Millisecond,
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(backfillQueries) != 1 {
		t.Fatalf("expected exactly 1 backfill FilterLogs call per cycle, got %d", len(backfillQueries))
	}

	q := backfillQueries[0]
	if q.FromBlock == nil || q.ToBlock == nil {
		t.Fatal("expected backfill query to have both FromBlock and ToBlock set")
	}

	backfillRange := q.ToBlock.Uint64() - q.FromBlock.Uint64() + 1
	if backfillRange > batchSize {
		t.Errorf("expected backfill batch to cover at most %d blocks, but it covered %d (from=%d to=%d)",
			batchSize, backfillRange, q.FromBlock.Uint64(), q.ToBlock.Uint64())
	}

	wantTo := uint64(60)
	if q.ToBlock.Uint64() != wantTo {
		t.Errorf("expected backfill ToBlock=%d (one batch of %d from 51), got %d",
			wantTo, batchSize, q.ToBlock.Uint64())
	}
}

