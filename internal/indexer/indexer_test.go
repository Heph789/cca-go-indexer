package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/eth"
	"github.com/cca/go-indexer/internal/store"
)

// Ensure interfaces are satisfied at compile time.
var _ eth.Client = (*mockEthClient)(nil)
var _ store.Store = (*mockStore)(nil)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// makeHeader creates a *types.Header with a deterministic hash.
func makeHeader(number uint64) *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(number),
		Extra:  []byte(fmt.Sprintf("block-%d", number)),
	}
}

func headerHash(number uint64) string {
	return makeHeader(number).Hash().Hex()
}

func defaultConfig() IndexerConfig {
	return IndexerConfig{
		ChainID:        1,
		StartBlock:     0,
		PollInterval:   time.Millisecond,
		BlockBatchSize: 100,
		Confirmations:  0,
		Addresses:      []common.Address{common.HexToAddress("0xaaa")},
	}
}

// ---------------------------------------------------------------------------
// Mock: eth.Client
// ---------------------------------------------------------------------------

type mockEthClient struct {
	blockNumberFn    func(ctx context.Context) (uint64, error)
	headerByNumberFn func(ctx context.Context, number *big.Int) (*types.Header, error)
	filterLogsFn     func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

func (m *mockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	return m.blockNumberFn(ctx)
}

func (m *mockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return m.headerByNumberFn(ctx, number)
}

func (m *mockEthClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return m.filterLogsFn(ctx, q)
}

func (m *mockEthClient) Close() {}

// ---------------------------------------------------------------------------
// Mock: store.Store and repos
// ---------------------------------------------------------------------------

type mockCursorRepo struct {
	blockNumber  uint64
	blockHash    string
	getErr       error
	upsertErr    error
	getCalled    int
	upsertCalled int
	lastUpsert   struct {
		chainID     int64
		blockNumber uint64
		blockHash   string
	}
}

func (m *mockCursorRepo) Get(_ context.Context, _ int64) (uint64, string, error) {
	m.getCalled++
	return m.blockNumber, m.blockHash, m.getErr
}

func (m *mockCursorRepo) Upsert(_ context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	m.upsertCalled++
	m.lastUpsert.chainID = chainID
	m.lastUpsert.blockNumber = blockNumber
	m.lastUpsert.blockHash = blockHash
	return m.upsertErr
}

type blockInsert struct {
	chainID     int64
	blockNumber uint64
	blockHash   string
	parentHash  string
}

type mockBlockRepo struct {
	hashes     map[uint64]string
	getHashErr error
	insertErr  error
	inserts    []blockInsert
}

func (m *mockBlockRepo) Insert(_ context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	m.inserts = append(m.inserts, blockInsert{chainID, blockNumber, blockHash, parentHash})
	return m.insertErr
}

func (m *mockBlockRepo) GetHash(_ context.Context, _ int64, blockNumber uint64) (string, error) {
	if m.getHashErr != nil {
		return "", m.getHashErr
	}
	return m.hashes[blockNumber], nil
}

func (m *mockBlockRepo) DeleteFrom(_ context.Context, _ int64, _ uint64) error {
	return nil
}

type mockAuctionRepo struct {
	insertCalled int
	deleteCalled int
}

func (m *mockAuctionRepo) Insert(_ context.Context, _ *cca.Auction) error {
	m.insertCalled++
	return nil
}

func (m *mockAuctionRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	m.deleteCalled++
	return nil
}

type mockRawEventRepo struct {
	insertCalled int
	deleteCalled int
}

func (m *mockRawEventRepo) Insert(_ context.Context, _ *cca.RawEvent) error {
	m.insertCalled++
	return nil
}

func (m *mockRawEventRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	m.deleteCalled++
	return nil
}

type mockStore struct {
	cursorRepo   *mockCursorRepo
	blockRepo    *mockBlockRepo
	auctionRepo  *mockAuctionRepo
	rawEventRepo *mockRawEventRepo
	withTxCalled int
	withTxErr    error
}

func (m *mockStore) CursorRepo() store.CursorRepository   { return m.cursorRepo }
func (m *mockStore) BlockRepo() store.BlockRepository      { return m.blockRepo }
func (m *mockStore) AuctionRepo() store.AuctionRepository  { return m.auctionRepo }
func (m *mockStore) RawEventRepo() store.RawEventRepository { return m.rawEventRepo }
func (m *mockStore) Close()                                 {}

func (m *mockStore) WithTx(_ context.Context, fn func(store.Store) error) error {
	m.withTxCalled++
	if m.withTxErr != nil {
		return m.withTxErr
	}
	return fn(m)
}

func newTestStore() *mockStore {
	return &mockStore{
		cursorRepo:   &mockCursorRepo{},
		blockRepo:    &mockBlockRepo{hashes: make(map[uint64]string)},
		auctionRepo:  &mockAuctionRepo{},
		rawEventRepo: &mockRawEventRepo{},
	}
}

// ---------------------------------------------------------------------------
// Mock: EventHandler
// ---------------------------------------------------------------------------

type mockHandler struct {
	eventID   common.Hash
	eventName string
	handled   []types.Log
}

func (m *mockHandler) EventName() string      { return m.eventName }
func (m *mockHandler) EventID() common.Hash   { return m.eventID }
func (m *mockHandler) Handle(_ context.Context, _ int64, log types.Log, _ store.Store) error {
	m.handled = append(m.handled, log)
	return nil
}

// ---------------------------------------------------------------------------
// newTestIndexer wires up a ChainIndexer with the given mocks.
// ---------------------------------------------------------------------------

func newTestIndexer(client *mockEthClient, s *mockStore, registry *HandlerRegistry, cfg IndexerConfig) *ChainIndexer {
	return New(client, s, registry, cfg, discardLogger())
}

// ===========================================================================
// Tests: Initialization
// ===========================================================================

func TestTick_LoadsCursorFromDB_FirstCall(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)

	// Set the stored hash so detectReorg passes.
	s.blockRepo.hashes[50] = headerHash(50)

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	atHead, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false when behind head")
	}
	if s.cursorRepo.getCalled != 1 {
		t.Fatalf("expected CursorRepo.Get called once, got %d", s.cursorRepo.getCalled)
	}
}

func TestTick_UsesStartBlock_WhenNoCursor(t *testing.T) {
	s := newTestStore()
	// CursorRepo returns (0, "", nil) — no cursor exists.

	var capturedQuery ethereum.FilterQuery
	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
			capturedQuery = q
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.StartBlock = 100

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	_, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Batch should start from StartBlock (100).
	if capturedQuery.FromBlock == nil || capturedQuery.FromBlock.Uint64() != 100 {
		t.Fatalf("expected FromBlock=100, got %v", capturedQuery.FromBlock)
	}
}

func TestTick_CachesCursor_SubsequentCalls(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.BlockBatchSize = 10
	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	// First tick — loads cursor.
	_, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	// Update block hashes for next tick's detectReorg.
	s.blockRepo.hashes[60] = headerHash(60)

	// Second tick — should use cached cursor.
	_, err = idx.tick(context.Background())
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}

	if s.cursorRepo.getCalled != 1 {
		t.Fatalf("expected CursorRepo.Get called once, got %d", s.cursorRepo.getCalled)
	}
}

// ===========================================================================
// Tests: Batch Calculation
// ===========================================================================

func TestTick_CalculatesBatchRange(t *testing.T) {
	tests := []struct {
		name     string
		cursor   uint64
		head     uint64
		batch    uint64
		wantFrom uint64
		wantTo   uint64
	}{
		{"full batch", 50, 200, 100, 51, 150},
		{"partial batch at head", 50, 80, 100, 51, 80},
		{"single block", 50, 51, 100, 51, 51},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore()
			s.cursorRepo.blockNumber = tt.cursor
			s.cursorRepo.blockHash = headerHash(tt.cursor)
			s.blockRepo.hashes[tt.cursor] = headerHash(tt.cursor)

			var capturedQuery ethereum.FilterQuery
			client := &mockEthClient{
				blockNumberFn: func(_ context.Context) (uint64, error) { return tt.head, nil },
				headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
					return makeHeader(n.Uint64()), nil
				},
				filterLogsFn: func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
					capturedQuery = q
					return nil, nil
				},
			}

			cfg := defaultConfig()
			cfg.BlockBatchSize = tt.batch

			handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
			idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

			_, err := idx.tick(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotFrom := capturedQuery.FromBlock.Uint64()
			gotTo := capturedQuery.ToBlock.Uint64()
			if gotFrom != tt.wantFrom || gotTo != tt.wantTo {
				t.Fatalf("batch range: got [%d, %d], want [%d, %d]", gotFrom, gotTo, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

// ===========================================================================
// Tests: At Head / Sleep
// ===========================================================================

func TestTick_ReturnsAtHead_WhenCursorAtSafeHead(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 100
	s.cursorRepo.blockHash = headerHash(100)

	filterLogsCalled := false
	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 112, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			filterLogsCalled = true
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.Confirmations = 12 // safeHead = 112 - 12 = 100 = cursor

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	atHead, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !atHead {
		t.Fatal("expected atHead=true when cursor == safeHead")
	}
	if filterLogsCalled {
		t.Fatal("FilterLogs should not be called when at head")
	}
}

func TestTick_SafeHeadUnderflowGuard(t *testing.T) {
	s := newTestStore()
	// No cursor — will use StartBlock=0, so cursor starts at 0.

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 3, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			t.Fatal("FilterLogs should not be called when safeHead would underflow")
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.Confirmations = 10 // safeHead would be 3 - 10 = underflow!

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	atHead, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !atHead {
		t.Fatal("expected atHead=true when safeHead underflows")
	}
}

// ===========================================================================
// Tests: Happy Path
// ===========================================================================

func TestTick_HappyPath_FetchesLogsAndAdvancesCursor(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	handlerTopic := common.HexToHash("0xdeadbeef")

	testLogs := []types.Log{
		{BlockNumber: 51, Topics: []common.Hash{handlerTopic}, TxHash: common.HexToHash("0x1")},
		{BlockNumber: 55, Topics: []common.Hash{handlerTopic}, TxHash: common.HexToHash("0x2")},
		{BlockNumber: 60, Topics: []common.Hash{handlerTopic}, TxHash: common.HexToHash("0x3")},
	}

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return testLogs, nil
		},
	}

	cfg := defaultConfig()
	cfg.BlockBatchSize = 10

	handler := &mockHandler{eventID: handlerTopic, eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	atHead, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false when behind head")
	}

	// Handler should have been called for each log.
	if len(handler.handled) != 3 {
		t.Fatalf("expected handler called 3 times, got %d", len(handler.handled))
	}

	// Block hashes should be inserted for each block in the batch (51-60).
	if len(s.blockRepo.inserts) != 10 {
		t.Fatalf("expected 10 block inserts, got %d", len(s.blockRepo.inserts))
	}

	// Cursor should advance to 60 (end of batch).
	if s.cursorRepo.upsertCalled != 1 {
		t.Fatalf("expected CursorRepo.Upsert called once, got %d", s.cursorRepo.upsertCalled)
	}
	if s.cursorRepo.lastUpsert.blockNumber != 60 {
		t.Fatalf("expected cursor at 60, got %d", s.cursorRepo.lastUpsert.blockNumber)
	}

	// All writes should happen inside a transaction.
	if s.withTxCalled != 1 {
		t.Fatalf("expected WithTx called once, got %d", s.withTxCalled)
	}
}

func TestTick_HappyPath_NoLogs(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.BlockBatchSize = 10

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	_, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Blocks still tracked even with no logs.
	if len(s.blockRepo.inserts) != 10 {
		t.Fatalf("expected 10 block inserts, got %d", len(s.blockRepo.inserts))
	}

	// Cursor still advances.
	if s.cursorRepo.upsertCalled != 1 {
		t.Fatalf("expected cursor upsert, got %d", s.cursorRepo.upsertCalled)
	}

	// Handler should NOT be called.
	if len(handler.handled) != 0 {
		t.Fatalf("expected no handler calls, got %d", len(handler.handled))
	}
}

func TestTick_PassesCorrectFilterQuery(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	addr1 := common.HexToAddress("0xaaa")
	addr2 := common.HexToAddress("0xbbb")
	topic1 := common.HexToHash("0x111")
	topic2 := common.HexToHash("0x222")

	var capturedQuery ethereum.FilterQuery
	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
			capturedQuery = q
			return nil, nil
		},
	}

	cfg := defaultConfig()
	cfg.Addresses = []common.Address{addr1, addr2}
	cfg.BlockBatchSize = 10

	h1 := &mockHandler{eventID: topic1, eventName: "Event1"}
	h2 := &mockHandler{eventID: topic2, eventName: "Event2"}
	registry := NewRegistry(h1, h2)
	idx := newTestIndexer(client, s, registry, cfg)

	_, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify addresses.
	if len(capturedQuery.Addresses) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(capturedQuery.Addresses))
	}

	// Verify topics match registry.
	expectedTopics := registry.TopicFilter()
	if len(capturedQuery.Topics) != len(expectedTopics) {
		t.Fatalf("expected %d topic groups, got %d", len(expectedTopics), len(capturedQuery.Topics))
	}
	if len(capturedQuery.Topics[0]) != 2 {
		t.Fatalf("expected 2 topics in filter, got %d", len(capturedQuery.Topics[0]))
	}
}

// ===========================================================================
// Tests: Error Propagation
// ===========================================================================

func TestTick_Error_BlockNumber(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)

	wantErr := errors.New("rpc error")
	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 0, wantErr },
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return nil, nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	_, err := idx.tick(context.Background())
	if err == nil {
		t.Fatal("expected error from BlockNumber")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped rpc error, got: %v", err)
	}
}

func TestTick_Error_FilterLogs(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	wantErr := errors.New("filter error")
	client := &mockEthClient{
		blockNumberFn:    func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) { return makeHeader(n.Uint64()), nil },
		filterLogsFn:     func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) { return nil, wantErr },
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	_, err := idx.tick(context.Background())
	if err == nil {
		t.Fatal("expected error from FilterLogs")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped filter error, got: %v", err)
	}
}

func TestTick_Error_HeaderByNumber(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)

	wantErr := errors.New("header error")
	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return nil, wantErr
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	_, err := idx.tick(context.Background())
	if err == nil {
		t.Fatal("expected error from HeaderByNumber")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped header error, got: %v", err)
	}
}

func TestTick_Error_WithTx(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 50
	s.cursorRepo.blockHash = headerHash(50)
	s.blockRepo.hashes[50] = headerHash(50)
	s.withTxErr = errors.New("tx error")

	client := &mockEthClient{
		blockNumberFn:    func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) { return makeHeader(n.Uint64()), nil },
		filterLogsFn:     func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) { return nil, nil },
	}

	cfg := defaultConfig()
	cfg.BlockBatchSize = 10

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	_, err := idx.tick(context.Background())
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	// Cursor should NOT be advanced on tx failure.
	if idx.cursor != 50 {
		t.Fatalf("cursor should not advance on WithTx error, got %d", idx.cursor)
	}
}

func TestTick_Error_CursorLoad(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.getErr = errors.New("db error")

	client := &mockEthClient{
		blockNumberFn:    func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) { return nil, nil },
		filterLogsFn:     func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) { return nil, nil },
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	_, err := idx.tick(context.Background())
	if err == nil {
		t.Fatal("expected error from CursorRepo.Get")
	}
}

// ===========================================================================
// Tests: Reorg Integration
// ===========================================================================

func TestTick_DetectsReorgAndResets(t *testing.T) {
	s := newTestStore()
	s.cursorRepo.blockNumber = 100
	s.cursorRepo.blockHash = headerHash(100)

	// Stored hash for block 100 does NOT match the chain's current hash.
	s.blockRepo.hashes[100] = "0xold_hash"

	// handleReorg walks back — block 95 is the common ancestor.
	for i := uint64(96); i <= 100; i++ {
		s.blockRepo.hashes[i] = "0xold_hash"
	}
	s.blockRepo.hashes[95] = headerHash(95)

	client := &mockEthClient{
		blockNumberFn: func(_ context.Context) (uint64, error) { return 200, nil },
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return makeHeader(n.Uint64()), nil
		},
		filterLogsFn: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			t.Fatal("FilterLogs should not be called during reorg handling")
			return nil, nil
		},
	}

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), defaultConfig())

	atHead, err := idx.tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atHead {
		t.Fatal("expected atHead=false after reorg (should re-process)")
	}
}

// ===========================================================================
// Tests: Context / Run
// ===========================================================================

func TestRun_StopsOnContextCancel(t *testing.T) {
	s := newTestStore()
	// At head — Run will sleep, giving us time to cancel.
	client := &mockEthClient{
		blockNumberFn:    func(_ context.Context) (uint64, error) { return 0, nil },
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) { return makeHeader(0), nil },
		filterLogsFn:     func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) { return nil, nil },
	}

	cfg := defaultConfig()
	cfg.PollInterval = time.Second // long enough that cancel fires first

	handler := &mockHandler{eventID: common.HexToHash("0x01"), eventName: "Test"}
	idx := newTestIndexer(client, s, NewRegistry(handler), cfg)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- idx.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}
