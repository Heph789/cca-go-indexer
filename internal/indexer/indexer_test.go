package indexer

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, nil))
}

// TestBatchRange verifies basic batch calculation.
// Chain head=100, cursor=50, batch=10, conf=0 → safeHead=100.
// Expected: FilterLogs from=51 to=60, newCursor=60, atHead=false.
func TestBatchRange(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10, // process up to 10 blocks per tick
		Confirmations:  0,  // no confirmation buffer
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=50: last processed block was 50
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
	if len(ethClient.filterLogsCalls) != 1 {
		t.Fatalf("expected 1 FilterLogs call, got %d", len(ethClient.filterLogsCalls))
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 51 { // cursor+1
		t.Fatalf("expected FromBlock=51, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 60 { // cursor+batch
		t.Fatalf("expected ToBlock=60, got %d", q.ToBlock.Uint64())
	}
}

// TestSafeHead verifies that confirmations reduce the effective head.
// Chain head=100, cursor=50, batch=10, conf=5 → safeHead=95.
// Batch still fits (51..60 < 95), so same range as TestBatchRange.
func TestSafeHead(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  5, // safeHead = 100 - 5 = 95
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=50: well behind safeHead=95, batch fits entirely
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
// chain head, tick treats it as "at head" rather than underflowing.
// Chain head=5, conf=10 → safeHead would be negative → clamp to 0.
// Cursor=0 >= safeHead=0, so nothing to do.
func TestSafeHeadUnderflowGuard(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 5} // very low chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  10, // conf > head → underflow guard must kick in
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=0: fresh start, but safeHead underflows so nothing to process
	_, atHead, err := idx.tick(context.Background(), 0)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if !atHead {
		t.Fatal("expected atHead=true when confirmations > head")
	}
	if len(ethClient.filterLogsCalls) != 0 {
		t.Fatalf("expected no FilterLogs calls, got %d", len(ethClient.filterLogsCalls))
	}
}

// TestNothingToDo verifies that when the cursor is already at the safe head,
// tick returns atHead=true and makes no RPC calls beyond BlockNumber.
// Chain head=100, cursor=100, conf=0 → safeHead=100, cursor >= safeHead.
func TestNothingToDo(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0, // safeHead = head = 100
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=100: already fully caught up
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
// Chain head=100, cursor=0, batch=10 → from=1 to=10, atHead=false.
// The atHead=false signals Run() to loop immediately without sleeping.
func TestCatchUp(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10, // first batch: blocks 1..10
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=0: no blocks processed yet, start from block 1
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
	if q.FromBlock.Uint64() != 1 { // cursor+1 = 1
		t.Fatalf("expected FromBlock=1, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 10 { // cursor+batch = 10
		t.Fatalf("expected ToBlock=10, got %d", q.ToBlock.Uint64())
	}
}

// TestDispatch verifies that logs returned by FilterLogs are dispatched
// to the correct EventHandler via the registry.
// 3 logs all share the same topic0 → the single registered handler
// should receive all 3 in its handleCalls slice.
func TestDispatch(t *testing.T) {
	topic := common.HexToHash("0xaa") // topic0 for our fake handler
	ethClient := &fakeEthClient{
		blockNumber: 100, // chain head
		filterLogsResult: []types.Log{
			{Topics: []common.Hash{topic}, BlockNumber: 51},
			{Topics: []common.Hash{topic}, BlockNumber: 52},
			{Topics: []common.Hash{topic}, BlockNumber: 53},
		},
	}
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: topic} // matches topic0
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=50: batch covers 51..60, which contains our 3 logs
	_, _, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if len(handler.handleCalls) != 3 {
		t.Fatalf("expected 3 handler calls, got %d", len(handler.handleCalls))
	}
}

// TestAtomicity verifies that all store writes (block inserts + cursor upsert)
// happen inside a WithTx transaction. The fakeStore tracks inTx=true during
// the WithTx callback; sub-repos record this flag on each call.
func TestAtomicity(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		BlockBatchSize: 10, // batch covers 51..60 → 10 block inserts
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=50: triggers a batch write
	_, _, err := idx.tick(context.Background(), 50)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}

	// Every block insert must have happened inside WithTx
	for i, call := range s.block.insertCalls {
		if !call.inTx {
			t.Fatalf("block insert %d: expected inTx=true", i)
		}
	}

	// The cursor upsert must also have happened inside WithTx
	if len(s.cursor.upsertCalls) != 1 {
		t.Fatalf("expected 1 cursor upsert, got %d", len(s.cursor.upsertCalls))
	}
	if !s.cursor.upsertCalls[0].inTx {
		t.Fatal("cursor upsert: expected inTx=true")
	}
}

// TestFirstRun verifies the StartBlock config when no cursor exists in the DB.
// Run() resolves cursor=0 to StartBlock-1 before calling tick().
// StartBlock=42, batch=10 → tick(cursor=41) → from=42 to=51.
func TestFirstRun(t *testing.T) {
	ethClient := &fakeEthClient{blockNumber: 100} // chain head
	s := newFakeStore()
	handler := &fakeEventHandler{eventID: common.HexToHash("0xaa")}
	registry := NewRegistry(handler)

	idx := New(ethClient, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     42, // first block to process on fresh start
		BlockBatchSize: 10,
		Confirmations:  0,
		PollInterval:   time.Second,
	}, newTestLogger())

	// cursor=41: simulates Run() resolving no-cursor to StartBlock-1
	newCursor, _, err := idx.tick(context.Background(), 41)
	if err != nil {
		t.Fatalf("tick() error: %v", err)
	}
	if newCursor != 51 { // 41 + batch(10) = 51
		t.Fatalf("expected newCursor=51, got %d", newCursor)
	}
	q := ethClient.filterLogsCalls[0].query
	if q.FromBlock.Uint64() != 42 { // StartBlock
		t.Fatalf("expected FromBlock=42, got %d", q.FromBlock.Uint64())
	}
	if q.ToBlock.Uint64() != 51 { // 41 + batch(10) = 51
		t.Fatalf("expected ToBlock=51, got %d", q.ToBlock.Uint64())
	}
}

// TestShutdown verifies that Run() exits cleanly on context cancellation.
// The context is cancelled before Run() is called, so it should return
// context.Canceled immediately without hanging.
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
	cancel() // cancel immediately — Run() should exit without entering the loop

	err := idx.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
