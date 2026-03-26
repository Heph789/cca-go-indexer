package indexer

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ---------------------------------------------------------------------------
// detectReorg tests
// ---------------------------------------------------------------------------

func TestDetectReorg_ReturnsFalseWhenHashesMatch(t *testing.T) {
	t.Parallel()

	storedHash := common.HexToHash("0xaaa")
	header := &types.Header{Number: big.NewInt(10), Nonce: types.BlockNonce{1}}
	// We need the stored hash to equal header.Hash(), so compute it first.
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
			_ = storedHash // suppress unused warning; we override below
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

// ---------------------------------------------------------------------------
// handleReorg tests
// ---------------------------------------------------------------------------

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, nil))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

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

	ancestor, err := handleReorg(context.Background(), newTestLogger(), client, ms, 324, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ancestor != 98 {
		t.Fatalf("expected ancestor 98, got %d", ancestor)
	}
}

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

	var deletedRawFrom, deletedAuctionFrom, deletedBlockFrom uint64
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
	ms.blockRepo.DeleteFromFn = func(_ context.Context, _ int64, from uint64) error {
		deletedBlockFrom = from
		return nil
	}
	ms.cursorRepo.UpsertFn = func(_ context.Context, _ int64, bn uint64, bh common.Hash) error {
		cursorBlock = bn
		cursorHash = bh
		return nil
	}

	_, err := handleReorg(context.Background(), newTestLogger(), client, ms, 324, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// rollbackFrom = ancestor(9) + 1 = 10
	if deletedRawFrom != 10 {
		t.Fatalf("expected raw events deleted from 10, got %d", deletedRawFrom)
	}
	if deletedAuctionFrom != 10 {
		t.Fatalf("expected auctions deleted from 10, got %d", deletedAuctionFrom)
	}
	if deletedBlockFrom != 10 {
		t.Fatalf("expected blocks deleted from 10, got %d", deletedBlockFrom)
	}
	if cursorBlock != 9 {
		t.Fatalf("expected cursor at 9, got %d", cursorBlock)
	}
	if cursorHash != headers[9].Hash() {
		t.Fatalf("expected cursor hash %s, got %s", headers[9].Hash(), cursorHash)
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

	ancestor, err := handleReorg(context.Background(), newTestLogger(), client, ms, 324, 7)
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

	_, err := handleReorg(context.Background(), newTestLogger(), client, ms, 324, 200)
	if err == nil {
		t.Fatal("expected error for exceeding max reorg depth")
	}
	if !errors.Is(err, nil) { // just check it has the right message
		// errors.Is won't match nil, so let's check the string
	}
	expected := "reorg deeper than 128 blocks"
	if err.Error() == "" || !containsStr(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got: %v", expected, err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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

	_, err := handleReorg(context.Background(), newTestLogger(), client, ms, 324, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotCursorHash != ancestorHash {
		t.Fatalf("expected cursor hash %s, got %s", ancestorHash, gotCursorHash)
	}
}
