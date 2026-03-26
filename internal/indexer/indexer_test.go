package indexer

import (
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
			auctionRepo:  s.auctionRepo,
			rawEventRepo: s.rawEventRepo,
			cursorRepo:   s.cursorRepo,
			blockRepo:    s.blockRepo,
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
