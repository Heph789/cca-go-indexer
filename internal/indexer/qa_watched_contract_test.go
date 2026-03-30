// QA Gate: Watched Contract Framework Verification (Issue #97)
//
// These end-to-end verification tests exercise the real ChainIndexer with mock
// dependencies to verify the watched contract framework. They are designed to:
//
//   - COMPILE on both the pre-Phase-1A branch (bid-auction-1) and the
//     Phase 1A branch (bid-auction-1-/watched-contract-repo-1)
//   - FAIL at runtime on the pre-Phase-1A branch (because the indexer does
//     not merge watched contract addresses or advance per-contract cursors)
//   - PASS on the Phase 1A branch
//
// The tests observe behavior through FilterLogs address captures. On the green
// branch, the indexer merges watched contract addresses from ListCaughtUp into
// the filter query. On the red branch, only config addresses appear.
package indexer

import (
	"context"
	"math/big"
	"sync"
	"testing"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ---------------------------------------------------------------------------
// Experiment 1: Multi-batch address merging
//
// The indexer starts at cursor=100 with chainHead=130, batchSize=10. On the
// green branch, the indexer calls ListCaughtUp which returns two watched
// contract addresses, so FilterLogs receives config + watched addresses.
// On the red branch, FilterLogs only receives config addresses.
//
// We verify that across 3 batches, FilterLogs always receives 3 addresses
// (1 config + 2 watched). This fails on the red branch because only 1 address
// (the config) is ever passed to FilterLogs.
// ---------------------------------------------------------------------------

func TestQA_MultiBatchAddressMerging(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 130, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	// Capture addresses from each FilterLogs call.
	var mu sync.Mutex
	var capturedAddrs [][]common.Address
	ctx, cancel := context.WithCancel(context.Background())

	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		capturedAddrs = append(capturedAddrs, q.Addresses)
		mu.Unlock()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 130 {
			cancel()
		}
		return nil
	}

	// On the green branch, the indexer will call ListCaughtUp which (via the
	// default mock) returns nil. We configure it to return watched addresses.
	// On the red branch, setupWatchedContracts is a no-op (field doesn't exist).
	setupWatchedContracts(s, []common.Address{
		common.HexToAddress("0xAUCTION1"),
		common.HexToAddress("0xAUCTION2"),
	})

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	// Should have processed 3 batches.
	if len(capturedAddrs) < 3 {
		t.Fatalf("expected at least 3 FilterLogs calls, got %d", len(capturedAddrs))
	}

	// Each batch should have 3 addresses (1 config + 2 watched).
	// On the red branch this fails: only 1 address (config) is present.
	for i, addrs := range capturedAddrs {
		if len(addrs) != 3 {
			t.Errorf("batch %d: expected 3 addresses (1 config + 2 watched), got %d: %v",
				i, len(addrs), addrs)
		}
	}
}

// ---------------------------------------------------------------------------
// Experiment 2: Contract added mid-stream
//
// During the first batch, the indexer has no watched contracts. A new contract
// appears during the second batch. We verify that:
//   - Batch 1: FilterLogs has 1 address (config only)
//   - Batch 2: FilterLogs has 2 addresses (config + new watched)
//
// On the red branch, both batches have only 1 address, so the second
// assertion fails.
// ---------------------------------------------------------------------------

func TestQA_ContractAddedMidStream(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	newWatched := common.HexToAddress("0xNEWAUCTION")

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 120, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	// Simulate a contract appearing after the first batch.
	var mu sync.Mutex
	listCallCount := 0
	setupWatchedContractsDynamic(s, func() []common.Address {
		mu.Lock()
		listCallCount++
		n := listCallCount
		mu.Unlock()
		if n == 1 {
			return nil
		}
		return []common.Address{newWatched}
	})

	var capturedAddrs [][]common.Address
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		capturedAddrs = append(capturedAddrs, q.Addresses)
		mu.Unlock()
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 120 {
			cancel()
		}
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedAddrs) < 2 {
		t.Fatalf("expected at least 2 FilterLogs calls, got %d", len(capturedAddrs))
	}

	// Batch 1: only config address (same on both branches).
	if len(capturedAddrs[0]) != 1 {
		t.Errorf("batch 1: expected 1 address (config only), got %d: %v",
			len(capturedAddrs[0]), capturedAddrs[0])
	}

	// Batch 2: config + new watched address (fails on red branch: still 1).
	if len(capturedAddrs[1]) != 2 {
		t.Errorf("batch 2: expected 2 addresses (config + watched), got %d: %v",
			len(capturedAddrs[1]), capturedAddrs[1])
	}
}

// ---------------------------------------------------------------------------
// Experiment 3: No watched contracts -- baseline behavior
//
// ListCaughtUp returns empty. FilterLogs should have only the config address
// across all batches. This test passes on BOTH branches to verify the
// baseline path is preserved.
// ---------------------------------------------------------------------------

func TestQA_NoWatchedContractsBaselineBehavior(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 130, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
	}

	// No watched contracts.
	setupWatchedContracts(s, nil)

	var mu sync.Mutex
	var capturedAddrs [][]common.Address
	ctx, cancel := context.WithCancel(context.Background())

	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		capturedAddrs = append(capturedAddrs, q.Addresses)
		mu.Unlock()
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 130 {
			cancel()
		}
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	for i, addrs := range capturedAddrs {
		if len(addrs) != 1 {
			t.Errorf("batch %d: expected 1 address, got %d", i, len(addrs))
		}
		if len(addrs) > 0 && addrs[0] != configAddr {
			t.Errorf("batch %d: expected config address %s, got %s",
				i, configAddr.Hex(), addrs[0].Hex())
		}
	}
}

// ---------------------------------------------------------------------------
// Experiment 4: Reorg recovery includes watched addresses in post-reorg batch
//
// A reorg is detected at block 100, with common ancestor at 99. After reorg
// recovery the indexer resumes at the ancestor block and processes a new batch.
// On the green branch, FilterLogs for that batch includes watched addresses
// because ListCaughtUp is called before each batch. On the red branch,
// FilterLogs only gets config addresses.
// ---------------------------------------------------------------------------

func TestQA_ReorgRecoveryIncludesWatchedAddresses(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 200, nil
	}

	reorgHeader := &types.Header{Number: big.NewInt(100), Nonce: types.BlockNonce{1}}
	ancestorHeader := &types.Header{Number: big.NewInt(99), Nonce: types.BlockNonce{2}}

	eth.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		switch n.Uint64() {
		case 100:
			return reorgHeader, nil
		case 99:
			return ancestorHeader, nil
		default:
			return &types.Header{ParentHash: common.HexToHash("0xparent")}, nil
		}
	}

	staleHash := common.HexToHash("0xdead000000000000000000000000000000000000000000000000000000000001")
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, blockNumber uint64) (common.Hash, error) {
		switch blockNumber {
		case 100:
			return staleHash, nil // differs -> reorg detected
		case 99:
			return ancestorHeader.Hash(), nil // matches -> common ancestor
		default:
			return common.Hash{}, nil
		}
	}

	// Set up watched contracts so they appear in post-reorg batch.
	watchedAddr := common.HexToAddress("0xWATCHED")
	setupWatchedContracts(s, []common.Address{watchedAddr})

	var mu sync.Mutex
	var capturedAddrs [][]common.Address
	ctx, cancel := context.WithCancel(context.Background())

	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		capturedAddrs = append(capturedAddrs, q.Addresses)
		mu.Unlock()
		cancel() // stop after first post-reorg batch
		return nil, nil
	}

	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, _ uint64, _ common.Hash) error {
		return nil
	}

	registry := NewRegistry(noopLogger())
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(capturedAddrs) < 1 {
		t.Fatal("expected at least 1 FilterLogs call after reorg recovery")
	}

	// After reorg recovery, the first batch should include watched addresses.
	// On red branch: only config address (1). On green branch: config + watched (2).
	if len(capturedAddrs[0]) != 2 {
		t.Errorf("post-reorg batch: expected 2 addresses (config + watched), got %d: %v",
			len(capturedAddrs[0]), capturedAddrs[0])
	}
}
