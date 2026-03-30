package indexer

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestDetectReorg_ReturnsFalseWhenHashesMatch(t *testing.T) {
	t.Parallel()

	header := &types.Header{Number: big.NewInt(10), Nonce: types.BlockNonce{1}}
	chainHash := header.Hash()

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			if n.Uint64() != 10 {
				t.Fatalf("unexpected block number %d", n.Uint64())
			}
			return header, nil
		},
	}

	blockRepo := &mockBlockRepo{
		GetHashFn: func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
			if bn != 10 {
				t.Fatalf("unexpected block number %d", bn)
			}
			return chainHash, nil
		},
	}

	got, err := detectReorg(context.Background(), client, blockRepo, 324, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatal("expected false, got true")
	}
}

func TestDetectReorg_ReturnsFalseWhenNoStoredHash(t *testing.T) {
	t.Parallel()

	client := &mockEthClient{} // should not be called
	blockRepo := &mockBlockRepo{
		GetHashFn: func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
			return common.Hash{}, nil // zero hash = not indexed yet
		},
	}

	got, err := detectReorg(context.Background(), client, blockRepo, 324, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatal("expected false, got true")
	}
}

func TestDetectReorg_ReturnsTrueWhenHashesDiffer(t *testing.T) {
	t.Parallel()

	header := &types.Header{Number: big.NewInt(10), Nonce: types.BlockNonce{1}}

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return header, nil
		},
	}

	blockRepo := &mockBlockRepo{
		GetHashFn: func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
			return common.HexToHash("0xdead"), nil // differs from header.Hash()
		},
	}

	got, err := detectReorg(context.Background(), client, blockRepo, 324, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Fatal("expected true, got false")
	}
}

func TestDetectReorg_ReturnsFalseAtBlock0(t *testing.T) {
	t.Parallel()

	client := &mockEthClient{} // should not be called
	blockRepo := &mockBlockRepo{}

	got, err := detectReorg(context.Background(), client, blockRepo, 324, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatal("expected false for genesis block, got true")
	}
}

func TestDetectReorg_PropagatesBlockRepoError(t *testing.T) {
	t.Parallel()

	repoErr := errors.New("db down")
	client := &mockEthClient{}
	blockRepo := &mockBlockRepo{
		GetHashFn: func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
			return common.Hash{}, repoErr
		},
	}

	_, err := detectReorg(context.Background(), client, blockRepo, 324, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected wrapped repoErr, got: %v", err)
	}
}

func TestDetectReorg_PropagatesEthClientError(t *testing.T) {
	t.Parallel()

	rpcErr := errors.New("rpc timeout")
	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, _ *big.Int) (*types.Header, error) {
			return nil, rpcErr
		},
	}
	blockRepo := &mockBlockRepo{
		GetHashFn: func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
			return common.HexToHash("0xabc"), nil // non-zero so we proceed to ethClient
		},
	}

	_, err := detectReorg(context.Background(), client, blockRepo, 324, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rpcErr) {
		t.Fatalf("expected wrapped rpcErr, got: %v", err)
	}
}

// makeHeaders creates a map of block number -> *types.Header with distinct hashes.
func makeHeaders(blocks ...uint64) map[uint64]*types.Header {
	m := make(map[uint64]*types.Header, len(blocks))
	for _, b := range blocks {
		m[b] = &types.Header{Number: big.NewInt(int64(b)), Nonce: types.BlockNonce{byte(b)}}
	}
	return m
}

func TestHandleReorg_FindsCommonAncestor(t *testing.T) {
	t.Parallel()

	// Blocks 97,98,99,100 exist. Reorg at 100. Common ancestor at 98.
	headers := makeHeaders(97, 98, 99, 100)

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			h, ok := headers[n.Uint64()]
			if !ok {
				t.Fatalf("unexpected header request for block %d", n.Uint64())
			}
			return h, nil
		},
	}

	ms := newMockStore()
	ms.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		switch bn {
		case 99: // mismatched
			return common.HexToHash("0xbad99"), nil
		case 98: // matches
			return headers[98].Hash(), nil
		default:
			t.Fatalf("unexpected GetHash for block %d", bn)
			return common.Hash{}, nil
		}
	}

	ancestor, err := handleReorg(context.Background(), noopLogger(), client, ms, 324, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor != 98 {
		t.Fatalf("expected ancestor 98, got %d", ancestor)
	}
}

// TestHandleReorg_RollsBackAllDataInTransaction verifies that handleReorg
// cascades the rollback to every repository — raw events, auctions, bids,
// checkpoints, watched contract cursors, and block records — using the correct
// fromBlock (ancestor + 1). Also verifies the global cursor is reset to the
// common ancestor block and hash.
func TestHandleReorg_RollsBackAllDataInTransaction(t *testing.T) {
	t.Parallel()

	headers := makeHeaders(8, 9, 10)

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return headers[n.Uint64()], nil
		},
	}

	ms := newMockStore()
	// Reorg at 10, ancestor at 9.
	ms.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		if bn == 9 {
			return headers[9].Hash(), nil
		}
		return common.Hash{}, nil // won't match for others, but 9 is first checked
	}

	var deletedRawFrom, deletedAuctionFrom, deletedBidFrom, deletedCheckpointFrom, deletedBlockFrom uint64
	var rollbackCursorsFrom uint64
	var rollbackCursorsCalled bool
	var cursorBlock uint64
	var cursorHash common.Hash

	ms.rawEventRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		deletedRawFrom = from
		return nil
	}
	ms.auctionRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		deletedAuctionFrom = from
		return nil
	}
	// Bid repo must be rolled back so that stale bids from the reorged
	// blocks do not persist after the chain reorganizes.
	ms.bidRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		deletedBidFrom = from
		return nil
	}
	// Checkpoint repo must be rolled back so that stale checkpoint records
	// from reorged blocks are purged.
	ms.checkpointRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		deletedCheckpointFrom = from
		return nil
	}
	// Watched contract cursors must be rolled back so that per-contract
	// last_indexed_block values do not exceed the new global cursor.
	ms.watchedContractRepo.RollbackCursorsFn = func(_ context.Context, _ int64, from uint64) error {
		rollbackCursorsCalled = true
		rollbackCursorsFrom = from
		return nil
	}
	ms.blockRepo.DeleteFromFn = func(_ context.Context, _ int64, from uint64) error {
		deletedBlockFrom = from
		return nil
	}
	ms.cursorRepo.UpsertFn = func(_ context.Context, _ int64, bn uint64, bh common.Hash) error {
		cursorBlock = bn
		cursorHash = bh
		return nil
	}

	_, err := handleReorg(context.Background(), noopLogger(), client, ms, 324, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// rollbackFrom = ancestor(9) + 1 = 10
	wantRollbackFrom := uint64(10)

	// --- raw events ---
	if deletedRawFrom != wantRollbackFrom {
		t.Fatalf("expected raw events deleted from %d, got %d", wantRollbackFrom, deletedRawFrom)
	}

	// --- auctions ---
	if deletedAuctionFrom != wantRollbackFrom {
		t.Fatalf("expected auctions deleted from %d, got %d", wantRollbackFrom, deletedAuctionFrom)
	}

	// --- bids ---
	if deletedBidFrom != wantRollbackFrom {
		t.Fatalf("expected bids deleted from %d, got %d", wantRollbackFrom, deletedBidFrom)
	}

	// --- checkpoints ---
	if deletedCheckpointFrom != wantRollbackFrom {
		t.Fatalf("expected checkpoints deleted from %d, got %d", wantRollbackFrom, deletedCheckpointFrom)
	}

	// --- watched contract cursors ---
	if !rollbackCursorsCalled {
		t.Fatal("expected WatchedContractRepo.RollbackCursors to be called")
	}
	if rollbackCursorsFrom != wantRollbackFrom {
		t.Fatalf("expected watched contract cursors rolled back from %d, got %d", wantRollbackFrom, rollbackCursorsFrom)
	}

	// --- block records ---
	if deletedBlockFrom != wantRollbackFrom {
		t.Fatalf("expected blocks deleted from %d, got %d", wantRollbackFrom, deletedBlockFrom)
	}

	// --- global cursor reset ---
	wantCursorBlock := uint64(9)
	wantCursorHash := headers[9].Hash()
	if cursorBlock != wantCursorBlock {
		t.Fatalf("expected cursor at %d, got %d", wantCursorBlock, cursorBlock)
	}
	if cursorHash != wantCursorHash {
		t.Fatalf("expected cursor hash %s, got %s", wantCursorHash, cursorHash)
	}
}

func TestHandleReorg_ReturnsCommonAncestorBlockNumber(t *testing.T) {
	t.Parallel()

	headers := makeHeaders(5, 6, 7)

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return headers[n.Uint64()], nil
		},
	}

	ms := newMockStore()
	ms.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		if bn == 5 {
			return headers[5].Hash(), nil
		}
		return common.HexToHash("0xbad"), nil
	}

	ancestor, err := handleReorg(context.Background(), noopLogger(), client, ms, 324, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor != 5 {
		t.Fatalf("expected ancestor 5, got %d", ancestor)
	}
}

func TestHandleReorg_ErrorWhenExceedsMaxReorgDepth(t *testing.T) {
	t.Parallel()

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return &types.Header{Number: n, Nonce: types.BlockNonce{1}}, nil
		},
	}

	ms := newMockStore()
	ms.blockRepo.GetHashFn = func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
		return common.HexToHash("0xnomatch"), nil // never matches
	}

	_, err := handleReorg(context.Background(), noopLogger(), client, ms, 324, 200)
	if err == nil {
		t.Fatal("expected error for exceeding max reorg depth")
	}
	if !strings.Contains(err.Error(), "reorg deeper than 128 blocks") {
		t.Fatalf("expected error containing %q, got: %v", "reorg deeper than 128 blocks", err)
	}
}

func TestHandleReorg_GetsAncestorHashForCursorUpdate(t *testing.T) {
	t.Parallel()

	headers := makeHeaders(7, 8)

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return headers[n.Uint64()], nil
		},
	}

	ms := newMockStore()

	ancestorHash := headers[7].Hash()

	// GetHash is called both during ancestor search AND during tx for cursor update
	ms.blockRepo.GetHashFn = func(_ context.Context, _ int64, bn uint64) (common.Hash, error) {
		if bn == 7 {
			return ancestorHash, nil
		}
		return common.HexToHash("0xbad"), nil
	}

	var gotCursorHash common.Hash
	ms.cursorRepo.UpsertFn = func(_ context.Context, _ int64, _ uint64, bh common.Hash) error {
		gotCursorHash = bh
		return nil
	}

	_, err := handleReorg(context.Background(), noopLogger(), client, ms, 324, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotCursorHash != ancestorHash {
		t.Fatalf("expected cursor hash %s, got %s", ancestorHash, gotCursorHash)
	}
}
