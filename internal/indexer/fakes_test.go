package indexer

// fakes_test.go provides in-memory test doubles (fakes) for the eth.Client and
// store.Store interfaces. These fakes are used across all indexer test files to
// test the indexer logic in isolation — no real Ethereum RPC or database needed.
//
// Design notes:
//   - Each fake records the calls it receives (e.g. filterLogsCalls, insertCalls)
//     so tests can assert on what was called and with what arguments.
//   - fakeStore tracks an `inTx` flag that is set to true inside WithTx. The
//     fakeBlockRepo and fakeCursorRepo capture this flag on each call, allowing
//     tests to verify that writes happen inside a transaction (see TestAtomicity).
//   - Fakes return canned data (e.g. fakeEthClient.blockNumber) rather than
//     computing it, keeping test setup simple and explicit.

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// ---------------------------------------------------------------------------
// fakeEthClient — implements eth.Client
// ---------------------------------------------------------------------------
// Provides canned responses for BlockNumber, HeaderByNumber, and FilterLogs.
// Records every FilterLogs call so tests can inspect the query parameters
// (FromBlock, ToBlock, Addresses, Topics) that the indexer sent.

// filterLogsCall captures the arguments of a single FilterLogs invocation.
type filterLogsCall struct {
	query ethereum.FilterQuery
}

type fakeEthClient struct {
	blockNumber      uint64     // value returned by BlockNumber()
	headers          map[uint64]*types.Header // optional per-block headers
	filterLogsResult []types.Log              // logs returned by every FilterLogs call
	filterLogsCalls  []filterLogsCall          // recorded calls for assertion
}

func (f *fakeEthClient) BlockNumber(_ context.Context) (uint64, error) {
	return f.blockNumber, nil
}

// HeaderByNumber returns a pre-configured header if one exists for the
// requested block number, otherwise generates a deterministic header via
// makeHeader. This lets most tests skip header setup entirely.
func (f *fakeEthClient) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	if f.headers != nil {
		if h, ok := f.headers[number.Uint64()]; ok {
			return h, nil
		}
	}
	return makeHeader(number.Uint64()), nil
}

// FilterLogs records the call and returns the canned filterLogsResult.
func (f *fakeEthClient) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	f.filterLogsCalls = append(f.filterLogsCalls, filterLogsCall{query: q})
	return f.filterLogsResult, nil
}

func (f *fakeEthClient) Close() {}

// ---------------------------------------------------------------------------
// fakeStore — implements store.Store
// ---------------------------------------------------------------------------
// Composes fake repos and tracks whether we're inside a WithTx callback.
// The inTx flag is propagated to fakeBlockRepo and fakeCursorRepo so tests
// can verify transactional writes (see TestAtomicity).

type fakeStore struct {
	cursor   *fakeCursorRepo
	block    *fakeBlockRepo
	auction  *fakeAuctionRepo
	rawEvent *fakeRawEventRepo
	inTx     bool // true while inside a WithTx callback
}

func newFakeStore() *fakeStore {
	s := &fakeStore{
		auction:  &fakeAuctionRepo{},
		rawEvent: &fakeRawEventRepo{},
	}
	// cursor and block repos need a back-pointer to the store so they can
	// read the inTx flag when recording calls.
	s.cursor = &fakeCursorRepo{store: s}
	s.block = &fakeBlockRepo{store: s}
	return s
}

func (f *fakeStore) AuctionRepo() store.AuctionRepository  { return f.auction }
func (f *fakeStore) RawEventRepo() store.RawEventRepository { return f.rawEvent }
func (f *fakeStore) CursorRepo() store.CursorRepository     { return f.cursor }
func (f *fakeStore) BlockRepo() store.BlockRepository        { return f.block }

// WithTx sets the inTx flag for the duration of fn, then clears it. This
// mirrors the real Store's behavior of running fn inside a DB transaction,
// but without any actual transaction — the flag is purely for test assertions.
func (f *fakeStore) WithTx(_ context.Context, fn func(store.Store) error) error {
	f.inTx = true
	defer func() { f.inTx = false }()
	return fn(f)
}

// ---------------------------------------------------------------------------
// fakeCursorRepo — implements store.CursorRepository
// ---------------------------------------------------------------------------
// Returns canned cursor values and records every Upsert call (including
// whether it happened inside a transaction) for assertion.

// cursorUpsertCall captures the arguments and transactional context of an Upsert.
type cursorUpsertCall struct {
	chainID     int64
	blockNumber uint64
	blockHash   string
	inTx        bool // was this call made inside WithTx?
}

type fakeCursorRepo struct {
	store       *fakeStore        // back-pointer to check inTx
	blockNumber uint64            // value returned by Get()
	blockHash   string            // value returned by Get()
	upsertCalls []cursorUpsertCall // recorded Upsert calls
}

// Get returns the canned cursor. Returns (0, "", nil) by default, which
// simulates a fresh start with no persisted cursor.
func (f *fakeCursorRepo) Get(_ context.Context, _ int64) (uint64, string, error) {
	return f.blockNumber, f.blockHash, nil
}

// Upsert records the call and captures the current inTx state.
func (f *fakeCursorRepo) Upsert(_ context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	f.upsertCalls = append(f.upsertCalls, cursorUpsertCall{
		chainID:     chainID,
		blockNumber: blockNumber,
		blockHash:   blockHash,
		inTx:        f.store.inTx,
	})
	return nil
}

// ---------------------------------------------------------------------------
// fakeBlockRepo — implements store.BlockRepository
// ---------------------------------------------------------------------------
// Records every Insert call (including transactional context) for assertion.

// blockInsertCall captures the arguments and transactional context of an Insert.
type blockInsertCall struct {
	chainID     int64
	blockNumber uint64
	blockHash   string
	parentHash  string
	inTx        bool // was this call made inside WithTx?
}

type fakeBlockRepo struct {
	store       *fakeStore       // back-pointer to check inTx
	insertCalls []blockInsertCall // recorded Insert calls
}

// Insert records the call and captures the current inTx state.
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

func (f *fakeBlockRepo) DeleteFrom(_ context.Context, _ int64, _ uint64) error {
	return nil
}

// ---------------------------------------------------------------------------
// fakeAuctionRepo — implements store.AuctionRepository
// ---------------------------------------------------------------------------
// Records every inserted Auction pointer so tests can inspect the decoded values.

type fakeAuctionRepo struct {
	insertCalls []*cca.Auction // every Auction passed to Insert
}

func (f *fakeAuctionRepo) Insert(_ context.Context, a *cca.Auction) error {
	f.insertCalls = append(f.insertCalls, a)
	return nil
}

func (f *fakeAuctionRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}

// ---------------------------------------------------------------------------
// fakeRawEventRepo — implements store.RawEventRepository
// ---------------------------------------------------------------------------
// Records every inserted RawEvent pointer so tests can inspect the raw data.

type fakeRawEventRepo struct {
	insertCalls []*cca.RawEvent // every RawEvent passed to Insert
}

func (f *fakeRawEventRepo) Insert(_ context.Context, e *cca.RawEvent) error {
	f.insertCalls = append(f.insertCalls, e)
	return nil
}

func (f *fakeRawEventRepo) DeleteFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}

// ---------------------------------------------------------------------------
// fakeEventHandler — implements EventHandler
// ---------------------------------------------------------------------------
// A configurable fake handler used to test the HandlerRegistry dispatch logic.
// Records every log it receives so tests can verify correct routing.

type fakeEventHandler struct {
	eventName   string       // returned by EventName()
	eventID     common.Hash  // returned by EventID() — this is the topic0 the registry matches on
	handleCalls []types.Log  // every log passed to Handle()
}

func (f *fakeEventHandler) EventName() string    { return f.eventName }
func (f *fakeEventHandler) EventID() common.Hash { return f.eventID }

// Handle records the log. Always returns nil (success).
func (f *fakeEventHandler) Handle(_ context.Context, _ int64, log types.Log, _ store.Store) error {
	f.handleCalls = append(f.handleCalls, log)
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeHeader generates a deterministic block header for a given block number.
// The parent hash is derived from (n-1), giving tests a consistent chain
// without needing to set up per-block headers.
func makeHeader(n uint64) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(n),
		ParentHash: common.BigToHash(new(big.Int).SetUint64(n - 1)),
	}
}
