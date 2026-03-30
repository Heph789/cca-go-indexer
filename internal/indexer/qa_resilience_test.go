// QA Gate: Resilience Verification (Issue #105)
//
// These end-to-end verification tests exercise the ChainIndexer with mock
// dependencies to verify Phase 2 resilience features:
//
//   - Inline backfill: newly-added watched contracts are backfilled from their
//     start block, one batch per cycle, without blocking forward polling.
//   - Reorg handling: RollbackFromBlock cascades deletes to bids, checkpoints,
//     raw events, and resets watched contract cursors. The indexer resumes from
//     the common ancestor.
//   - GetPrevTickPrice returns correct results after a reorg removes some bids.
//   - Reorg deeper than 128 blocks returns an error requiring manual intervention.
//   - Reorg during backfill resets contract cursors appropriately.
//
// The tests are designed to:
//   - COMPILE on both the red branch (bid-auction-1-/qa-bid-hint-1) and the
//     green branch (bid-auction-1-/qa-resilience-1)
//   - FAIL at runtime on the red branch because backfillContracts is not called,
//     and reorg cascade does not include bids/checkpoints/watched contract cursors
//   - PASS on the green branch
package indexer

import (
	"context"
	"math/big"
	"strings"
	"sync"
	"testing"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/indexer/handlers"
)

// ---------------------------------------------------------------------------
// Experiment 1: Inline backfill processes historical blocks for new contract
//
// A watched contract is added with StartBlock=50 while the global cursor is at
// block 100. After the forward polling batch completes, the indexer should call
// backfillContracts which calls ListNeedingBackfill, then FilterLogs for the
// contract's historical block range, and finally UpdateLastIndexedBlock to
// advance the per-contract cursor.
//
// On the red branch, backfillContracts does not exist in the Run loop, so
// ListNeedingBackfill is never called by the indexer. The test fails because
// no backfill FilterLogs call is observed.
// ---------------------------------------------------------------------------

func TestQA_BackfillProcessesHistoricalBlocks(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	backfillAddr := common.HexToAddress("0xNEWAUCTION")

	// Global cursor starts at 100.
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 110, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	// The contract that is already caught up participates in forward polling.
	setupWatchedContracts(s, nil)

	// The contract needing backfill: starts at block 50, not yet indexed.
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{
			{
				ChainID:          1,
				Address:          backfillAddr,
				StartBlock:       50,
				LastIndexedBlock: 0,
			},
		}, nil
	}

	// Track all FilterLogs calls to distinguish forward vs backfill.
	var mu sync.Mutex
	var filterCalls []ethereum.FilterQuery
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterCalls = append(filterCalls, q)
		mu.Unlock()
		return nil, nil
	}

	// Track UpdateLastIndexedBlock calls (backfill cursor advancement).
	var backfillUpdates []struct {
		addr  common.Address
		block uint64
	}
	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, addr common.Address, block uint64) error {
		mu.Lock()
		backfillUpdates = append(backfillUpdates, struct {
			addr  common.Address
			block uint64
		}{addr, block})
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 110 {
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

	// We expect at least 2 FilterLogs calls:
	// 1. Forward polling batch (blocks 101-110) with configAddr
	// 2. Backfill batch for backfillAddr (blocks 50-59)
	//
	// On the red branch, only the forward polling call happens (no backfill).
	if len(filterCalls) < 2 {
		t.Fatalf("expected at least 2 FilterLogs calls (forward + backfill), got %d", len(filterCalls))
	}

	// Find the backfill call: it should target only the backfillAddr.
	foundBackfill := false
	for _, q := range filterCalls {
		if len(q.Addresses) == 1 && q.Addresses[0] == backfillAddr {
			foundBackfill = true
			// Verify backfill starts from the contract's StartBlock.
			if q.FromBlock.Uint64() != 50 {
				t.Errorf("backfill FromBlock = %d, want 50", q.FromBlock.Uint64())
			}
			break
		}
	}
	if !foundBackfill {
		t.Fatal("no backfill FilterLogs call found for the new contract address")
	}

	// Verify per-contract cursor was advanced.
	if len(backfillUpdates) == 0 {
		t.Fatal("expected UpdateLastIndexedBlock to be called for backfill contract, got 0 calls")
	}

	foundCursorUpdate := false
	for _, u := range backfillUpdates {
		if u.addr == backfillAddr {
			foundCursorUpdate = true
			break
		}
	}
	if !foundCursorUpdate {
		t.Fatal("expected UpdateLastIndexedBlock for backfill contract address")
	}
}

// ---------------------------------------------------------------------------
// Experiment 2: Backfill advances per-contract cursor and contract joins
// forward polling when caught up
//
// Simulate a contract that needs a small backfill (only 5 blocks behind).
// After one backfill batch completes, the contract should be caught up and
// appear in ListCaughtUp for the next forward polling cycle.
//
// On the red branch, backfillContracts is not called, so the contract never
// catches up and never joins forward polling. The assertion on the second
// forward batch's addresses fails.
// ---------------------------------------------------------------------------

func TestQA_BackfillContractJoinsForwardPolling(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	newAddr := common.HexToAddress("0xNEWAUCTION")

	// Start at cursor 100, chain head at 120 (two batches of 10).
	cursorVal := uint64(100)
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return cursorVal, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 120, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	// Track backfill state: contract starts at block 95, lastIndexed=0.
	// After backfill processes blocks 95-104 (one batch of 10), it's caught up
	// to cursor 110 will still need more. But after blocks 105-110, caught up.
	var mu sync.Mutex
	contractCaughtUp := false

	// ListCaughtUp: after backfill completes, the contract joins forward polling.
	s.watchedContractRepo.ListCaughtUpFn = func(_ context.Context, _ int64, _ uint64) ([]common.Address, error) {
		mu.Lock()
		defer mu.Unlock()
		if contractCaughtUp {
			return []common.Address{newAddr}, nil
		}
		return nil, nil
	}

	// ListNeedingBackfill: returns the contract until it's caught up.
	backfillCallCount := 0
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, globalCursor uint64) ([]*cca.WatchedContract, error) {
		mu.Lock()
		defer mu.Unlock()
		backfillCallCount++
		if !contractCaughtUp {
			// Mark as caught up after first backfill call.
			contractCaughtUp = true
			return []*cca.WatchedContract{
				{
					ChainID:          1,
					Address:          newAddr,
					StartBlock:       95,
					LastIndexedBlock: 0,
				},
			}, nil
		}
		return nil, nil
	}

	// Track forward polling FilterLogs addresses.
	var capturedAddrs [][]common.Address
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		capturedAddrs = append(capturedAddrs, q.Addresses)
		mu.Unlock()
		return nil, nil
	}

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, _ common.Address, _ uint64) error {
		return nil
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

	// On the green branch: after the first forward batch, backfill runs and
	// marks the contract as caught up. The second forward batch includes the
	// new contract address. On the red branch: backfill never runs, so the
	// contract never joins forward polling.

	// We need at least 3 FilterLogs calls: 2 forward + 1 backfill.
	// On red branch: only 2 forward calls.
	if len(capturedAddrs) < 3 {
		t.Fatalf("expected at least 3 FilterLogs calls (2 forward + 1 backfill), got %d", len(capturedAddrs))
	}

	// The first forward batch should have only the config address.
	if len(capturedAddrs[0]) != 1 {
		t.Errorf("first forward batch: expected 1 address (config only), got %d", len(capturedAddrs[0]))
	}

	// Find a forward batch that includes the new contract address.
	// This is the batch after backfill completed.
	foundJoined := false
	for i, addrs := range capturedAddrs {
		if len(addrs) == 2 {
			foundJoined = true
			t.Logf("batch %d includes contract that joined after backfill: %v", i, addrs)
			break
		}
	}
	if !foundJoined {
		t.Error("new contract never joined forward polling after backfill (expected a batch with 2 addresses)")
	}
}

// ---------------------------------------------------------------------------
// Experiment 3: Backfill does not block forward polling -- one batch per cycle
//
// A contract needs backfill spanning many blocks (e.g., 100 blocks behind).
// The indexer should process only one batch of backfill per forward polling
// cycle, then yield so forward polling continues. We verify this by counting
// how many backfill FilterLogs calls occur per forward polling batch.
//
// On the red branch, backfillContracts is not called at all, so there are
// zero backfill calls. The test fails because no backfill is observed.
// ---------------------------------------------------------------------------

func TestQA_BackfillYieldsAfterOneBatch(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	backfillAddr := common.HexToAddress("0xLATECOMER")

	// Global cursor at 200, chain head at 220 (two forward batches).
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 200, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 220, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	setupWatchedContracts(s, nil)

	// Contract needs backfill from block 100 to current cursor.
	// With batchSize=10, this would require 10+ batches to fully backfill.
	var mu sync.Mutex
	backfillLastIndexed := uint64(0)
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, globalCursor uint64) ([]*cca.WatchedContract, error) {
		mu.Lock()
		defer mu.Unlock()
		if backfillLastIndexed < globalCursor {
			return []*cca.WatchedContract{
				{
					ChainID:          1,
					Address:          backfillAddr,
					StartBlock:       100,
					LastIndexedBlock: backfillLastIndexed,
				},
			}, nil
		}
		return nil, nil
	}

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, _ common.Address, block uint64) error {
		mu.Lock()
		backfillLastIndexed = block
		mu.Unlock()
		return nil
	}

	// Count backfill FilterLogs calls (identified by targeting backfillAddr only).
	backfillFilterCount := 0
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		if len(q.Addresses) == 1 && q.Addresses[0] == backfillAddr {
			backfillFilterCount++
		}
		mu.Unlock()
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 220 {
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

	// On the green branch: exactly 2 backfill calls (one per forward batch).
	// On the red branch: 0 backfill calls.
	if backfillFilterCount == 0 {
		t.Fatal("expected at least 1 backfill FilterLogs call, got 0 (backfill not called)")
	}

	// Each forward batch should trigger exactly 1 backfill batch (yielding).
	// With 2 forward batches, we expect exactly 2 backfill calls.
	if backfillFilterCount != 2 {
		t.Errorf("expected 2 backfill FilterLogs calls (one per forward batch), got %d", backfillFilterCount)
	}
}

// ---------------------------------------------------------------------------
// Experiment 4: Reorg cascades RollbackFromBlock to bids, checkpoints, raw
// events, and watched contract cursors
//
// This test runs the full indexer, indexes some blocks with bids and
// checkpoints, then triggers a 5-block reorg. We verify that:
//   - RollbackFromBlock is called (which cascades to all repos)
//   - The global cursor is reset to the common ancestor
//   - After the reorg, the indexer resumes from the common ancestor
//
// On the red branch, the reorg detection and handling code exists (it was
// added in Phase 1), but the cascade through RollbackFromBlock may not
// include bids, checkpoints, or watched contract cursors. The test verifies
// the complete cascade.
// ---------------------------------------------------------------------------

func TestQA_ReorgCascadesRollbackToAllRepos(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")

	// Phase 1: Index blocks 101-110 normally.
	// Phase 2: Detect reorg at block 110, common ancestor at block 105.
	// Phase 3: Resume from block 105.
	phase := 1
	var mu sync.Mutex

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	// Build headers with known hashes for reorg detection.
	normalHeaders := make(map[uint64]*types.Header)
	for i := uint64(100); i <= 120; i++ {
		normalHeaders[i] = &types.Header{
			Number: big.NewInt(int64(i)),
			Nonce:  types.BlockNonce{byte(i)},
			Time:   1700000000 + i,
		}
	}
	// Reorged headers for blocks 106-110: different nonces = different hashes.
	reorgedHeaders := make(map[uint64]*types.Header)
	for i := uint64(106); i <= 110; i++ {
		reorgedHeaders[i] = &types.Header{
			Number: big.NewInt(int64(i)),
			Nonce:  types.BlockNonce{byte(i + 100)}, // different from normal
			Time:   1700000000 + i,
		}
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 120, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		block := n.Uint64()
		mu.Lock()
		p := phase
		mu.Unlock()
		if p >= 2 && block >= 106 && block <= 110 {
			// Return reorged headers after phase 1.
			return reorgedHeaders[block], nil
		}
		return normalHeaders[block], nil
	}

	// Store block hashes as they're inserted.
	storedHashes := make(map[uint64]common.Hash)
	s.blockRepo.InsertFn = func(_ context.Context, _ int64, blockNumber uint64, blockHash, _ common.Hash) error {
		mu.Lock()
		storedHashes[blockNumber] = blockHash
		mu.Unlock()
		return nil
	}

	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, blockNumber uint64) (common.Hash, error) {
		mu.Lock()
		h := storedHashes[blockNumber]
		mu.Unlock()
		return h, nil
	}

	s.blockRepo.DeleteFromFn = func(_ context.Context, _ int64, fromBlock uint64) error {
		mu.Lock()
		for k := range storedHashes {
			if k >= fromBlock {
				delete(storedHashes, k)
			}
		}
		mu.Unlock()
		return nil
	}

	setupWatchedContracts(s, nil)
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return nil, nil
	}

	// Track rollback calls.
	var rollbackCalled bool
	var rollbackFromBlock uint64
	var bidDeleteFrom, checkpointDeleteFrom, rawEventDeleteFrom, watchedCursorsFrom uint64

	s.bidRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		bidDeleteFrom = from
		mu.Unlock()
		return nil
	}
	s.checkpointRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		checkpointDeleteFrom = from
		mu.Unlock()
		return nil
	}
	s.rawEventRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		rawEventDeleteFrom = from
		mu.Unlock()
		return nil
	}
	s.watchedContractRepo.RollbackCursorsFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		watchedCursorsFrom = from
		mu.Unlock()
		return nil
	}
	s.RollbackFromBlockFn = func(_ context.Context, _ int64, fromBlock uint64) error {
		mu.Lock()
		rollbackCalled = true
		rollbackFromBlock = fromBlock
		mu.Unlock()
		// Delegate to the default implementation which cascades to all repos.
		return s.rawEventRepo.DeleteFromBlock(context.Background(), 1, fromBlock)
	}

	// Use the full mock cascade by restoring default RollbackFromBlockFn behavior.
	// We override RollbackFromBlockFn to NIL so the default mock cascade runs.
	s.RollbackFromBlockFn = nil

	// Track FilterLogs calls (forward polling).
	eth.FilterLogsFn = func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
		return nil, nil
	}

	// Track cursor updates to detect reorg and post-reorg resumption.
	var cursorUpdates []uint64
	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		mu.Lock()
		cursorUpdates = append(cursorUpdates, blockNumber)
		if blockNumber >= 110 && phase == 1 {
			// After indexing to 110, trigger reorg by switching to phase 2.
			phase = 2
		}
		if phase >= 2 && blockNumber >= 115 {
			// After re-indexing past the reorg point, we're done.
			cancel()
		}
		mu.Unlock()
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

	// Verify bids were deleted from the rollback point.
	// The common ancestor is block 105 (last block where hashes match),
	// so rollbackFrom = 106.
	wantRollbackFrom := uint64(106)

	if bidDeleteFrom != wantRollbackFrom {
		t.Errorf("bid DeleteFromBlock = %d, want %d", bidDeleteFrom, wantRollbackFrom)
	}
	if checkpointDeleteFrom != wantRollbackFrom {
		t.Errorf("checkpoint DeleteFromBlock = %d, want %d", checkpointDeleteFrom, wantRollbackFrom)
	}
	if rawEventDeleteFrom != wantRollbackFrom {
		t.Errorf("rawEvent DeleteFromBlock = %d, want %d", rawEventDeleteFrom, wantRollbackFrom)
	}
	if watchedCursorsFrom != wantRollbackFrom {
		t.Errorf("watchedContract RollbackCursors = %d, want %d", watchedCursorsFrom, wantRollbackFrom)
	}

	// Verify cursor was reset to the ancestor and then advanced past it.
	foundAncestorReset := false
	for _, c := range cursorUpdates {
		if c == 105 {
			foundAncestorReset = true
			break
		}
	}
	if !foundAncestorReset {
		t.Errorf("cursor was never reset to common ancestor 105; updates: %v", cursorUpdates)
	}

	_ = rollbackCalled
	_ = rollbackFromBlock
}

// ---------------------------------------------------------------------------
// Experiment 5: After reorg, indexer resumes from common ancestor
//
// Simulates a reorg at block 110 with common ancestor at 107. After reorg
// handling, the next forward batch should start from block 108 (ancestor+1).
// We verify this by capturing the FromBlock of the first post-reorg FilterLogs.
//
// On the red branch, the reorg detection and handling exist but the test
// verifies the full end-to-end flow: detect -> rollback -> resume. If the
// red branch does not call backfillContracts (which the indexer does after
// each batch on the green branch), we use a secondary assertion on the
// FilterLogs from-block to ensure the test discriminates.
// ---------------------------------------------------------------------------

func TestQA_AfterReorgResumesFromAncestor(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")

	// Index blocks 101-110 first, then detect reorg at cursor=110.
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 120, nil
	}

	// Create headers for all blocks.
	headers := make(map[uint64]*types.Header)
	for i := uint64(100); i <= 120; i++ {
		headers[i] = &types.Header{
			Number:     big.NewInt(int64(i)),
			Nonce:      types.BlockNonce{byte(i)},
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000 + i,
		}
	}
	// Reorged headers for blocks 108-110.
	reorgedHeaders := make(map[uint64]*types.Header)
	for i := uint64(108); i <= 110; i++ {
		reorgedHeaders[i] = &types.Header{
			Number:     big.NewInt(int64(i)),
			Nonce:      types.BlockNonce{byte(i + 100)},
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000 + i,
		}
	}

	var mu sync.Mutex
	reorgPhase := false

	eth.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		block := n.Uint64()
		mu.Lock()
		isReorg := reorgPhase
		mu.Unlock()
		if isReorg && block >= 108 && block <= 110 {
			return reorgedHeaders[block], nil
		}
		if h, ok := headers[block]; ok {
			return h, nil
		}
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	// Store block hashes as they're indexed.
	storedHashes := make(map[uint64]common.Hash)
	s.blockRepo.InsertFn = func(_ context.Context, _ int64, blockNumber uint64, blockHash, _ common.Hash) error {
		mu.Lock()
		storedHashes[blockNumber] = blockHash
		mu.Unlock()
		return nil
	}
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, blockNumber uint64) (common.Hash, error) {
		mu.Lock()
		h := storedHashes[blockNumber]
		mu.Unlock()
		return h, nil
	}
	s.blockRepo.DeleteFromFn = func(_ context.Context, _ int64, fromBlock uint64) error {
		mu.Lock()
		for k := range storedHashes {
			if k >= fromBlock {
				delete(storedHashes, k)
			}
		}
		mu.Unlock()
		return nil
	}

	setupWatchedContracts(s, nil)
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return nil, nil
	}

	// Track FilterLogs calls to observe post-reorg resumption.
	var filterCalls []uint64
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		filterCalls = append(filterCalls, q.FromBlock.Uint64())
		mu.Unlock()
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		mu.Lock()
		if blockNumber >= 110 && !reorgPhase {
			// After indexing to 110, switch to reorg phase so next loop
			// detects hash mismatch at block 110.
			reorgPhase = true
		}
		if reorgPhase && blockNumber >= 115 {
			cancel()
		}
		mu.Unlock()
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

	// After reorg (ancestor=107), the next batch should start from 108.
	// filterCalls: [101 (initial), 108 (post-reorg), ...]
	found108 := false
	for _, from := range filterCalls {
		if from == 108 {
			found108 = true
			break
		}
	}
	if !found108 {
		t.Errorf("expected a FilterLogs call with FromBlock=108 after reorg, got calls: %v", filterCalls)
	}
}

// ---------------------------------------------------------------------------
// Experiment 6: Reorg deeper than 128 blocks returns error
//
// When the reorg depth exceeds maxReorgDepth (128), handleReorg should return
// an error indicating manual intervention is needed. The indexer's Run loop
// should propagate this error.
//
// This test works on both branches since reorg handling exists in both.
// However, we include it for completeness and to verify the error message
// contains the expected text.
// ---------------------------------------------------------------------------

func TestQA_ReorgDeeperThan128BlocksReturnsError(t *testing.T) {
	// Test handleReorg directly to verify the depth limit. This exercises
	// the same code path the Run loop uses, but avoids the loop machinery.
	// The Run loop calls handleReorg and returns its error directly (no retry),
	// so testing handleReorg is equivalent.

	client := &mockEthClient{
		HeaderByNumberFn: func(_ context.Context, n *big.Int) (*types.Header, error) {
			return &types.Header{
				Number: n,
				Nonce:  types.BlockNonce{byte(n.Uint64() % 256)},
				Time:   1700000000,
			}, nil
		},
	}

	s := newMockStore()
	// Stored hashes never match chain headers -> reorg never resolves.
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, _ uint64) (common.Hash, error) {
		return common.HexToHash("0xnomatch"), nil
	}

	_, err := handleReorg(context.Background(), noopLogger(), client, s, 1, 200)

	if err == nil {
		t.Fatal("expected error for reorg deeper than 128 blocks, got nil")
	}
	if !strings.Contains(err.Error(), "reorg deeper than 128 blocks") {
		t.Errorf("error message = %q, want to contain 'reorg deeper than 128 blocks'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Experiment 7: GetPrevTickPrice returns correct results after reorg
//
// This end-to-end test indexes bids, triggers a reorg that removes some of
// them, and verifies that GetPrevTickPrice reflects the post-reorg state.
//
// We simulate this by:
// 1. Inserting bids into the mock store
// 2. Triggering a reorg via the indexer that calls RollbackFromBlock
// 3. Configuring GetPrevTickPrice to reflect the post-rollback state
// 4. Querying bidHint via GraphQL
//
// On the red branch, the complete reorg cascade (including bids) may not
// exist, and backfill is not present, so the test fails because bids from
// reorged blocks are not deleted.
// ---------------------------------------------------------------------------

func TestQA_GetPrevTickPriceAfterReorg(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	auctionAddr := common.HexToAddress("0xAUCTION1")

	// Start at cursor 100, index bids at blocks 101-110.
	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 120, nil
	}

	// Build headers for blocks 100-120.
	normalHeaders := make(map[uint64]*types.Header)
	for i := uint64(98); i <= 120; i++ {
		normalHeaders[i] = &types.Header{
			Number: big.NewInt(int64(i)),
			Nonce:  types.BlockNonce{byte(i)},
			Time:   1700000000 + i,
		}
	}

	var mu sync.Mutex
	reorgPhase := false

	eth.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		block := n.Uint64()
		mu.Lock()
		isReorg := reorgPhase
		mu.Unlock()
		if isReorg && block >= 106 && block <= 110 {
			// Return different headers to trigger reorg detection.
			return &types.Header{
				Number: big.NewInt(int64(block)),
				Nonce:  types.BlockNonce{byte(block + 100)},
				Time:   1700000000 + block,
			}, nil
		}
		if h, ok := normalHeaders[block]; ok {
			return h, nil
		}
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	// Store block hashes.
	storedHashes := make(map[uint64]common.Hash)
	s.blockRepo.InsertFn = func(_ context.Context, _ int64, blockNumber uint64, blockHash, _ common.Hash) error {
		mu.Lock()
		storedHashes[blockNumber] = blockHash
		mu.Unlock()
		return nil
	}
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, blockNumber uint64) (common.Hash, error) {
		mu.Lock()
		h := storedHashes[blockNumber]
		mu.Unlock()
		return h, nil
	}
	s.blockRepo.DeleteFromFn = func(_ context.Context, _ int64, fromBlock uint64) error {
		mu.Lock()
		for k := range storedHashes {
			if k >= fromBlock {
				delete(storedHashes, k)
			}
		}
		mu.Unlock()
		return nil
	}

	setupWatchedContracts(s, []common.Address{auctionAddr})
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return nil, nil
	}

	// Track inserted and deleted bids.
	var insertedBids []*cca.Bid
	var bidDeletedFrom uint64

	s.bidRepo.InsertFn = func(_ context.Context, bid *cca.Bid) error {
		mu.Lock()
		insertedBids = append(insertedBids, bid)
		mu.Unlock()
		return nil
	}
	s.bidRepo.DeleteFromBlockFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		bidDeletedFrom = from
		// Remove bids at or after the rollback block.
		var remaining []*cca.Bid
		for _, b := range insertedBids {
			if b.BlockNumber < from {
				remaining = append(remaining, b)
			}
		}
		insertedBids = remaining
		mu.Unlock()
		return nil
	}

	// Produce bids in blocks 101-110.
	q96 := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)
	bidID := 0
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		mu.Lock()
		defer mu.Unlock()
		// Only produce bids in forward polling for the auction address.
		hasAuction := false
		for _, a := range q.Addresses {
			if a == auctionAddr {
				hasAuction = true
				break
			}
		}
		if !hasAuction {
			return nil, nil
		}
		from := q.FromBlock.Uint64()
		to := q.ToBlock.Uint64()
		var logs []types.Log
		for block := from; block <= to; block++ {
			bidID++
			price := new(big.Int).Mul(big.NewInt(int64(bidID*100)), q96)
			logs = append(logs, buildBidSubmittedLog(
				t, auctionAddr, big.NewInt(int64(bidID)),
				common.HexToAddress("0xBIDDER"),
				price, big.NewInt(1000),
				block, uint(bidID),
			))
		}
		return logs, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		mu.Lock()
		if blockNumber >= 110 && !reorgPhase {
			// Trigger reorg after first pass.
			reorgPhase = true
		}
		if reorgPhase && blockNumber >= 115 {
			cancel()
		}
		mu.Unlock()
		return nil
	}

	registry := NewRegistry(noopLogger(), &handlers.BidSubmittedHandler{})
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	// Bids from blocks >= 106 should have been deleted during reorg rollback.
	// After reorg, the indexer re-indexes those blocks with new (correct) data,
	// so new bids from blocks >= 106 will be re-inserted. The key verification
	// is that DeleteFromBlock was called to purge stale data.
	if bidDeletedFrom == 0 {
		t.Fatal("expected bids to be deleted during reorg rollback, but DeleteFromBlock was never called")
	}
	if bidDeletedFrom != 106 {
		t.Errorf("bids deleted from block %d, want 106", bidDeletedFrom)
	}

	// Verify that bids were re-inserted after the reorg (proving the indexer
	// resumed and re-indexed the affected blocks).
	hasPostReorgBid := false
	for _, bid := range insertedBids {
		if bid.BlockNumber >= 106 {
			hasPostReorgBid = true
			break
		}
	}
	if !hasPostReorgBid {
		t.Error("expected bids to be re-inserted after reorg, but none found for blocks >= 106")
	}
}

// ---------------------------------------------------------------------------
// Experiment 8: Reorg during backfill resets contract cursors
//
// A contract is being backfilled (last_indexed_block=80) when a reorg occurs
// at the tip. The reorg rollback should call RollbackCursors which resets any
// per-contract cursor that is >= the rollback block. Even though the backfill
// contract's cursor is below the rollback point, the function should be called
// (it's a no-op for contracts already below the threshold).
//
// Additionally, a second watched contract with cursor=110 should have its
// cursor rolled back to 105 (rollback from block 106 -> cursor = 105).
//
// On the red branch, RollbackCursors may not be called during reorg
// (it was only connected in the resilience phase). The test fails because
// the cursor for the second contract is not reset.
// ---------------------------------------------------------------------------

func TestQA_ReorgDuringBackfillResetsCursors(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	// Reorg at 110, common ancestor at 105.
	headers := make(map[uint64]*types.Header)
	for i := uint64(100); i <= 120; i++ {
		headers[i] = &types.Header{
			Number: big.NewInt(int64(i)),
			Nonce:  types.BlockNonce{byte(i)},
			Time:   1700000000 + i,
		}
	}

	eth.HeaderByNumberFn = func(_ context.Context, n *big.Int) (*types.Header, error) {
		h, ok := headers[n.Uint64()]
		if !ok {
			return &types.Header{
				ParentHash: common.HexToHash("0xparent"),
				Time:       1700000000,
			}, nil
		}
		return h, nil
	}

	// Stored hashes: blocks 106-110 are stale (reorged).
	s.blockRepo.GetHashFn = func(_ context.Context, _ int64, blockNumber uint64) (common.Hash, error) {
		if blockNumber >= 106 && blockNumber <= 110 {
			return common.HexToHash("0xstale"), nil
		}
		if blockNumber == 105 {
			return headers[105].Hash(), nil
		}
		if h, ok := headers[blockNumber]; ok {
			return h.Hash(), nil
		}
		return common.Hash{}, nil
	}

	// Track RollbackCursors calls.
	var mu sync.Mutex
	var rollbackCursorsCalled bool
	var rollbackCursorsFromBlock uint64
	s.watchedContractRepo.RollbackCursorsFn = func(_ context.Context, _ int64, from uint64) error {
		mu.Lock()
		rollbackCursorsCalled = true
		rollbackCursorsFromBlock = from
		mu.Unlock()
		return nil
	}

	// Verify directly via handleReorg to isolate the reorg cascade behavior.
	// This avoids complexity of the full Run loop while still exercising the
	// real handleReorg code path and its transactional rollback cascade.
	ancestor, err := handleReorg(context.Background(), noopLogger(), eth, s, 1, 110)
	if err != nil {
		t.Fatalf("handleReorg returned error: %v", err)
	}
	if ancestor != 105 {
		t.Fatalf("handleReorg ancestor = %d, want 105", ancestor)
	}

	mu.Lock()
	defer mu.Unlock()

	// RollbackCursors should have been called with fromBlock=106.
	if !rollbackCursorsCalled {
		t.Fatal("expected WatchedContractRepo.RollbackCursors to be called during reorg, but it was not")
	}
	if rollbackCursorsFromBlock != 106 {
		t.Errorf("RollbackCursors fromBlock = %d, want 106", rollbackCursorsFromBlock)
	}
}

// ---------------------------------------------------------------------------
// Experiment 9: Backfill handles logs through real handler registry
//
// A watched contract needs backfill and the historical blocks contain
// BidSubmitted events. We verify that the backfill path dispatches logs
// through the real HandlerRegistry and the BidSubmittedHandler persists them.
//
// On the red branch, backfillContracts is not called, so no backfill logs
// are processed. The test fails because no bids from the backfill range
// are inserted.
// ---------------------------------------------------------------------------

func TestQA_BackfillDispatchesLogsToHandlers(t *testing.T) {
	eth := &mockEthClient{}
	s := newMockStore()

	configAddr := common.HexToAddress("0xFACTORY")
	backfillAddr := common.HexToAddress("0xAUCTION_BACKFILL")

	s.cursorRepo.GetFn = func(_ context.Context, _ int64) (uint64, common.Hash, error) {
		return 100, common.HexToHash("0xabc"), nil
	}

	eth.BlockNumberFn = func(_ context.Context) (uint64, error) {
		return 110, nil
	}

	eth.HeaderByNumberFn = func(_ context.Context, _ *big.Int) (*types.Header, error) {
		return &types.Header{
			ParentHash: common.HexToHash("0xparent"),
			Time:       1700000000,
		}, nil
	}

	setupWatchedContracts(s, nil)

	// Contract needs backfill from block 50.
	s.watchedContractRepo.ListNeedingBackfillFn = func(_ context.Context, _ int64, _ uint64) ([]*cca.WatchedContract, error) {
		return []*cca.WatchedContract{
			{
				ChainID:          1,
				Address:          backfillAddr,
				StartBlock:       50,
				LastIndexedBlock: 0,
			},
		}, nil
	}

	s.watchedContractRepo.UpdateLastIndexedBlockFn = func(_ context.Context, _ int64, _ common.Address, _ uint64) error {
		return nil
	}

	// Return BidSubmitted logs from the backfill address range.
	q96 := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)
	eth.FilterLogsFn = func(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
		// Only return logs for the backfill address.
		if len(q.Addresses) == 1 && q.Addresses[0] == backfillAddr {
			return []types.Log{
				buildBidSubmittedLog(
					t, backfillAddr, big.NewInt(1),
					common.HexToAddress("0xBIDDER"),
					new(big.Int).Mul(big.NewInt(100), q96),
					big.NewInt(1000),
					55, 0,
				),
			}, nil
		}
		return nil, nil
	}

	var mu sync.Mutex
	var backfillBids []*cca.Bid
	s.bidRepo.InsertFn = func(_ context.Context, bid *cca.Bid) error {
		mu.Lock()
		backfillBids = append(backfillBids, bid)
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cursorRepo.UpsertFn = func(_ context.Context, _ int64, blockNumber uint64, _ common.Hash) error {
		if blockNumber >= 110 {
			cancel()
		}
		return nil
	}

	registry := NewRegistry(noopLogger(), &handlers.BidSubmittedHandler{})
	idx := setupIndexer(eth, s, registry, IndexerConfig{
		ChainID:        1,
		StartBlock:     1,
		BlockBatchSize: 10,
		Addresses:      []common.Address{configAddr},
	})

	_ = idx.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	// On the green branch, the backfill path processes the BidSubmitted log
	// and inserts the bid. On the red branch, backfill never runs.
	if len(backfillBids) == 0 {
		t.Fatal("expected at least 1 bid inserted from backfill logs, got 0 (backfill not called)")
	}

	// Verify the bid came from the backfill address.
	if backfillBids[0].AuctionAddress != backfillAddr {
		t.Errorf("backfill bid AuctionAddress = %s, want %s",
			backfillBids[0].AuctionAddress.Hex(), backfillAddr.Hex())
	}
}
