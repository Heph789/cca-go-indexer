package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// ---------------------------------------------------------------------------
// Test helpers (duplicated from indexer_test.go — intentionally independent)
// ---------------------------------------------------------------------------

func reorgDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func reorgMakeHeader(number uint64) *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(number),
		Extra:  []byte(fmt.Sprintf("block-%d", number)),
	}
}

func reorgMakeReorgedHeader(number uint64) *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(number),
		Extra:  []byte(fmt.Sprintf("reorged-block-%d", number)),
	}
}

func reorgHeaderHash(number uint64) string {
	return reorgMakeHeader(number).Hash().Hex()
}

// ---------------------------------------------------------------------------
// Mocks for reorg tests
// ---------------------------------------------------------------------------

type reorgMockEthClient struct {
	headerByNumberFn func(ctx context.Context, number *big.Int) (*types.Header, error)
}

func (m *reorgMockEthClient) BlockNumber(_ context.Context) (uint64, error) { return 0, nil }
func (m *reorgMockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return m.headerByNumberFn(ctx, number)
}
func (m *reorgMockEthClient) FilterLogs(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
	return nil, nil
}
func (m *reorgMockEthClient) Close() {}

type reorgMockBlockRepo struct {
	hashes       map[uint64]string
	getHashErr   error
	insertCalled int
	deletedFrom  []uint64
	deleteErr    error
}

func (m *reorgMockBlockRepo) Insert(_ context.Context, _ int64, _ uint64, _, _ string) error {
	m.insertCalled++
	return nil
}

func (m *reorgMockBlockRepo) GetHash(_ context.Context, _ int64, blockNumber uint64) (string, error) {
	if m.getHashErr != nil {
		return "", m.getHashErr
	}
	return m.hashes[blockNumber], nil
}

func (m *reorgMockBlockRepo) DeleteFrom(_ context.Context, _ int64, fromBlock uint64) error {
	m.deletedFrom = append(m.deletedFrom, fromBlock)
	return m.deleteErr
}

type reorgMockCursorRepo struct {
	upsertCalled int
	lastUpsert   struct {
		chainID     int64
		blockNumber uint64
		blockHash   string
	}
	upsertErr error
}

func (m *reorgMockCursorRepo) Get(_ context.Context, _ int64) (uint64, string, error) {
	return 0, "", nil
}

func (m *reorgMockCursorRepo) Upsert(_ context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	m.upsertCalled++
	m.lastUpsert.chainID = chainID
	m.lastUpsert.blockNumber = blockNumber
	m.lastUpsert.blockHash = blockHash
	return m.upsertErr
}

type reorgMockAuctionRepo struct {
	deletedFrom []uint64
	deleteErr   error
}

func (m *reorgMockAuctionRepo) Insert(_ context.Context, _ *cca.Auction) error { return nil }
func (m *reorgMockAuctionRepo) DeleteFromBlock(_ context.Context, _ int64, fromBlock uint64) error {
	m.deletedFrom = append(m.deletedFrom, fromBlock)
	return m.deleteErr
}

type reorgMockRawEventRepo struct {
	deletedFrom []uint64
	deleteErr   error
}

func (m *reorgMockRawEventRepo) Insert(_ context.Context, _ *cca.RawEvent) error { return nil }
func (m *reorgMockRawEventRepo) DeleteFromBlock(_ context.Context, _ int64, fromBlock uint64) error {
	m.deletedFrom = append(m.deletedFrom, fromBlock)
	return m.deleteErr
}

type reorgMockStore struct {
	cursorRepo   *reorgMockCursorRepo
	blockRepo    *reorgMockBlockRepo
	auctionRepo  *reorgMockAuctionRepo
	rawEventRepo *reorgMockRawEventRepo
	withTxCalled int
	withTxErr    error
}

func (m *reorgMockStore) CursorRepo() store.CursorRepository    { return m.cursorRepo }
func (m *reorgMockStore) BlockRepo() store.BlockRepository       { return m.blockRepo }
func (m *reorgMockStore) AuctionRepo() store.AuctionRepository   { return m.auctionRepo }
func (m *reorgMockStore) RawEventRepo() store.RawEventRepository { return m.rawEventRepo }
func (m *reorgMockStore) Close()                                  {}

func (m *reorgMockStore) WithTx(_ context.Context, fn func(store.Store) error) error {
	m.withTxCalled++
	if m.withTxErr != nil {
		return m.withTxErr
	}
	return fn(m)
}

func newReorgTestStore() *reorgMockStore {
	return &reorgMockStore{
		cursorRepo:   &reorgMockCursorRepo{},
		blockRepo:    &reorgMockBlockRepo{hashes: make(map[uint64]string)},
		auctionRepo:  &reorgMockAuctionRepo{},
		rawEventRepo: &reorgMockRawEventRepo{},
	}
}

// ===========================================================================
// Tests: detectReorg
// ===========================================================================

func TestDetectReorg_NoReorg(t *testing.T) {
	blockRepo := &reorgMockBlockRepo{
		hashes: map[uint64]string{50: reorgHeaderHash(50)},
	}

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	reorged, err := detectReorg(context.Background(), client, blockRepo, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reorged {
		t.Fatal("expected no reorg when hashes match")
	}
}

func TestDetectReorg_Reorg(t *testing.T) {
	blockRepo := &reorgMockBlockRepo{
		hashes: map[uint64]string{50: "0xold_hash"},
	}

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	reorged, err := detectReorg(context.Background(), client, blockRepo, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reorged {
		t.Fatal("expected reorg when hashes differ")
	}
}

func TestDetectReorg_NoStoredHash(t *testing.T) {
	blockRepo := &reorgMockBlockRepo{
		hashes: map[uint64]string{}, // no hash stored for block 50
	}

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	reorged, err := detectReorg(context.Background(), client, blockRepo, 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reorged {
		t.Fatal("expected no reorg when no stored hash (first time indexing)")
	}
}

func TestDetectReorg_Error_BlockRepo(t *testing.T) {
	blockRepo := &reorgMockBlockRepo{
		getHashErr: errors.New("db error"),
	}

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return reorgMakeHeader(50), nil
		},
	}

	_, err := detectReorg(context.Background(), client, blockRepo, 1, 50)
	if err == nil {
		t.Fatal("expected error from BlockRepo.GetHash")
	}
}

func TestDetectReorg_Error_EthClient(t *testing.T) {
	blockRepo := &reorgMockBlockRepo{
		hashes: map[uint64]string{50: reorgHeaderHash(50)},
	}

	wantErr := errors.New("rpc error")
	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return nil, wantErr
		},
	}

	_, err := detectReorg(context.Background(), client, blockRepo, 1, 50)
	if err == nil {
		t.Fatal("expected error from HeaderByNumber")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped rpc error, got: %v", err)
	}
}

// ===========================================================================
// Tests: handleReorg
// ===========================================================================

func TestHandleReorg_FindsAncestorAndRollsBack(t *testing.T) {
	s := newReorgTestStore()

	// Blocks 103-105 are reorged, 102 is the common ancestor.
	for i := uint64(103); i <= 105; i++ {
		s.blockRepo.hashes[i] = "0xold_hash"
	}
	s.blockRepo.hashes[102] = reorgHeaderHash(102)

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	ancestor, err := handleReorg(context.Background(), reorgDiscardLogger(), client, s, 1, 105)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor != 102 {
		t.Fatalf("expected ancestor=102, got %d", ancestor)
	}

	// Verify atomic rollback happened.
	if s.withTxCalled != 1 {
		t.Fatalf("expected WithTx called once, got %d", s.withTxCalled)
	}

	// All repos should delete from ancestor+1 = 103.
	if len(s.rawEventRepo.deletedFrom) != 1 || s.rawEventRepo.deletedFrom[0] != 103 {
		t.Fatalf("expected RawEventRepo.DeleteFromBlock(103), got %v", s.rawEventRepo.deletedFrom)
	}
	if len(s.auctionRepo.deletedFrom) != 1 || s.auctionRepo.deletedFrom[0] != 103 {
		t.Fatalf("expected AuctionRepo.DeleteFromBlock(103), got %v", s.auctionRepo.deletedFrom)
	}
	if len(s.blockRepo.deletedFrom) != 1 || s.blockRepo.deletedFrom[0] != 103 {
		t.Fatalf("expected BlockRepo.DeleteFrom(103), got %v", s.blockRepo.deletedFrom)
	}

	// Cursor should be reset to ancestor.
	if s.cursorRepo.upsertCalled != 1 {
		t.Fatalf("expected CursorRepo.Upsert called once, got %d", s.cursorRepo.upsertCalled)
	}
	if s.cursorRepo.lastUpsert.blockNumber != 102 {
		t.Fatalf("expected cursor reset to 102, got %d", s.cursorRepo.lastUpsert.blockNumber)
	}
}

func TestHandleReorg_SingleBlockReorg(t *testing.T) {
	s := newReorgTestStore()

	// Block 100 is reorged, 99 is the ancestor.
	s.blockRepo.hashes[100] = "0xold_hash"
	s.blockRepo.hashes[99] = reorgHeaderHash(99)

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	ancestor, err := handleReorg(context.Background(), reorgDiscardLogger(), client, s, 1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor != 99 {
		t.Fatalf("expected ancestor=99, got %d", ancestor)
	}
}

func TestHandleReorg_MaxDepthExceeded(t *testing.T) {
	s := newReorgTestStore()

	// All blocks have mismatched hashes — no ancestor found within 128.
	for i := uint64(0); i <= 200; i++ {
		s.blockRepo.hashes[i] = "0xold_hash"
	}

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	_, err := handleReorg(context.Background(), reorgDiscardLogger(), client, s, 1, 200)
	if err == nil {
		t.Fatal("expected error when reorg exceeds max depth")
	}
}

func TestHandleReorg_Error_DuringWalkback(t *testing.T) {
	s := newReorgTestStore()
	s.blockRepo.hashes[100] = "0xold_hash"
	s.blockRepo.hashes[99] = "0xold_hash"

	wantErr := errors.New("rpc error")
	callCount := 0
	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			callCount++
			if callCount >= 2 {
				return nil, wantErr
			}
			return reorgMakeReorgedHeader(100), nil
		},
	}

	_, err := handleReorg(context.Background(), reorgDiscardLogger(), client, s, 1, 100)
	if err == nil {
		t.Fatal("expected error during walkback")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped rpc error, got: %v", err)
	}
}

func TestHandleReorg_Error_DuringRollback(t *testing.T) {
	s := newReorgTestStore()
	s.blockRepo.hashes[100] = "0xold_hash"
	s.blockRepo.hashes[99] = reorgHeaderHash(99)
	s.withTxErr = errors.New("tx error")

	client := &reorgMockEthClient{
		headerByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return reorgMakeHeader(n.Uint64()), nil
		},
	}

	_, err := handleReorg(context.Background(), reorgDiscardLogger(), client, s, 1, 100)
	if err == nil {
		t.Fatal("expected error from WithTx during rollback")
	}
}
