package indexer

// indexer_test.go tests the core tick() and Run() methods of ChainIndexer.
//
// All tests use in-memory fakes (fakeEthClient, fakeStore) so they run
// instantly with no external dependencies. The fakes are defined in
// fakes_test.go.
//
// Test strategy:
//   - Each test exercises one specific behavior of tick() — batch range
//     calculation, confirmation depth, underflow protection, dispatch, etc.
//   - Tests verify the returned values (newCursor, atHead, err) AND the
//     RPC calls made (via fakeEthClient.filterLogsCalls) to ensure the
//     indexer issues the correct eth_getLogs query.
//   - TestAtomicity verifies that all store writes happen inside a
//     transaction by checking the inTx flag captured by the fake repos.
//   - TestShutdown verifies graceful exit on context cancellation.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// newTestLogger returns a no-op logger that discards all output.
// Used to satisfy ChainIndexer's logger dependency without cluttering test output.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, nil))
}

// TestBatchRange verifies basic batch calculation.
//
// Setup:  chain head=100, cursor=50, batch=10, confirmations=0
// Expect: safeHead=100, range=[51, 60], newCursor=60, atHead=false
//
// The batch size caps how many blocks are processed per tick. With cursor=50
// and batch=10, the indexer should process blocks 51 through 60 (cursor+1
// through cursor+batch).
func TestBatchRange(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	newCursor, atHead, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false")
	}
	if newCursor != 60 {
		t.Fatalf("expected newCursor=60, got %d", newCursor)
	}

	// Verify the eth_getLogs query targeted the correct block range.
	if len(ethClient.filterLogsCalls) != 1 {
		t.Fatalf("expected 1 FilterLogs call, got %d", len(ethClient.filterLogsCalls))
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 51 {
		t.Fatalf("expected FromBlock=51, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 60 {
		t.Fatalf("expected ToBlock=60, got %d", q.ToBlock.Uint64())
	}
}

// TestSafeHead verifies that confirmations reduce the effective head.
//
// Setup:  chain head=100, cursor=50, batch=10, confirmations=5
// Expect: safeHead=95. Since cursor+batch=60 < 95, the batch fits and
//         the result is identical to TestBatchRange (range=[51,60]).
//
// This tests the "confirmation depth" feature: the indexer won't process
// blocks within `confirmations` of the chain tip, reducing reorg risk.
func TestSafeHead(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  5, // safeHead = 100 - 5 = 95
		PollInterval:   time.Second,
	}, newTestLogger())

	newCursor, atHead, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false")
	}
	if newCursor != 60 {
		t.Fatalf("expected newCursor=60, got %d", newCursor)
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 51 {
		t.Fatalf("expected FromBlock=51, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 60 {
		t.Fatalf("expected ToBlock=60, got %d", q.ToBlock.Uint64())
	}
}

// TestSafeHeadUnderflowGuard verifies that when confirmations exceed the
// chain head, the indexer treats it as "at head" rather than underflowing.
//
// Setup:  chain head=5, confirmations=10
// Expect: safeHead would be -5 (negative), but since we use uint64 the code
//         clamps to 0. cursor=0 >= safeHead=0, so there's nothing to process.
//
// Without this guard, head - confirmations would underflow to a huge uint64,
// causing the indexer to try processing billions of blocks.
func TestSafeHeadUnderflowGuard(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 5}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  10, // exceeds chain head
		PollInterval:   time.Second,
	}, newTestLogger())

	_, atHead, err := idx.tick(context.Background(), 0)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if !atHead {
		t.Fatal("expected atHead=true when confirmations > head")
	}
	// No FilterLogs call should be made — there are no safe blocks to process.
	if len(ethClient.filterLogsCalls) != 0 {
		t.Fatalf("expected no FilterLogs calls, got %d", len(ethClient.filterLogsCalls))
	}
}

// TestNothingToDo verifies that when the cursor is already at the safe head,
// tick returns atHead=true and makes no RPC calls beyond BlockNumber.
//
// Setup:  chain head=100, cursor=100, confirmations=0
// Expect: safeHead=100, cursor >= safeHead → nothing to process.
//
// This is the steady-state behavior: the indexer is caught up and waiting
// for new blocks. Run() will sleep for PollInterval before the next tick.
func TestNothingToDo(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	_, atHead, err := idx.tick(context.Background(), 100)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if !atHead {
		t.Fatal("expected atHead=true when cursor == head")
	}
	if len(ethClient.filterLogsCalls) != 0 {
		t.Fatalf("expected no FilterLogs calls, got %d", len(ethClient.filterLogsCalls))
	}
}

// TestCatchUp verifies catching up from genesis (cursor=0).
//
// Setup:  chain head=100, cursor=0, batch=10
// Expect: range=[1, 10], newCursor=10, atHead=false
//
// When atHead=false, Run() loops immediately without sleeping, allowing
// the indexer to catch up as fast as possible by processing consecutive
// batches (1-10, 11-20, 21-30, ...) until it reaches the safe head.
func TestCatchUp(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	newCursor, atHead, err := idx.tick(context.Background(), 0)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false when catching up")
	}
	if newCursor != 10 {
		t.Fatalf("expected newCursor=10, got %d", newCursor)
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 1 {
		t.Fatalf("expected FromBlock=1, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 10 {
		t.Fatalf("expected ToBlock=10, got %d", q.ToBlock.Uint64())
	}
}

// TestDispatch verifies that logs returned by FilterLogs are dispatched
// to the correct EventHandler via the registry.
//
// Setup:  3 logs all share the same topic0 matching the registered handler.
// Expect: the handler's handleCalls slice has 3 entries after tick().
//
// This tests the integration between tick() → FilterLogs → HandleLog → handler.
// The fake handler simply records the logs it receives; the real
// AuctionCreatedHandler would decode and persist them.
func TestDispatch(t *testing.T) {
	topic := common.HexToHash("0xaa")
	ethClient := &fakeEthClient{
		blockNumber: 100,
		filterLogsResult: []types.Log{
			{Topics: []common.Hash{topic}, BlockNumber: 51},
			{Topics: []common.Hash{topic}, BlockNumber: 52},
			{Topics: []common.Hash{topic}, BlockNumber: 53},
		},
	}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: topic}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	_, _, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if len(handler.handleCalls) != 3 {
		t.Fatalf("expected 3 handler calls, got %d", len(handler.handleCalls))
	}
}

// TestAtomicity verifies that all store writes (block inserts + cursor upsert)
// happen inside a WithTx transaction.
//
// How it works: fakeStore sets inTx=true during WithTx. The fake repos
// capture this flag on each call. After tick(), we check that every recorded
// call has inTx=true — proving the writes happened inside the transaction.
//
// Setup:  cursor=50, batch=10 → 10 blocks (51-60) + 1 cursor upsert
// Expect: all 10 block inserts and the cursor upsert have inTx=true
func TestAtomicity(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	_, _, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}

	// Every block insert must have happened inside the WithTx callback.
	for i, call := range s.block.insertCalls {
		if !call.inTx {
			t.Fatalf("block insert %d: expected inTx=true", i)
		}
	}

	// The cursor upsert must also have happened inside the transaction.
	if len(s.cursor.upsertCalls) != 1 {
		t.Fatalf("expected 1 cursor upsert, got %d", len(s.cursor.upsertCalls))
	}
	if !s.cursor.upsertCalls[0].inTx {
		t.Fatal("cursor upsert: expected inTx=true")
	}
}

// TestFirstRun verifies the StartBlock config when no cursor exists in the DB.
//
// In Run(), when cursor=0 (no persisted state) and StartBlock > 0, the cursor
// is initialized to StartBlock-1 so the first tick processes from StartBlock.
//
// Setup:  StartBlock=42, batch=10 → tick(cursor=41) → range=[42, 51]
// Expect: newCursor=51, FromBlock=42 (the configured StartBlock)
//
// We call tick() directly with cursor=41 to simulate the adjustment that
// Run() would make.
func TestFirstRun(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     42,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	newCursor, _, err := idx.tick(context.Background(), 41)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if newCursor != 51 {
		t.Fatalf("expected newCursor=51, got %d", newCursor)
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 42 {
		t.Fatalf("expected FromBlock=42, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 51 {
		t.Fatalf("expected ToBlock=51, got %d", q.ToBlock.Uint64())
	}
}

// TestShutdown verifies that Run() exits cleanly on context cancellation.
//
// The context is cancelled before Run() is called. Run() checks for
// cancellation via `select { case <-ctx.Done() }` at the top of its loop,
// so it should return context.Canceled immediately without processing any
// blocks or hanging.
func TestShutdown(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — Run() should exit on the first loop iteration

	err := idx.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
