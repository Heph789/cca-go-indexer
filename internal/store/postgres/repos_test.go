package postgres

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

func truncateAll(t *testing.T, s store.Store) {
	t.Helper()
	ps := s.(*pgStore)
	ctx := context.Background()
	_, err := ps.pool.Exec(ctx, "TRUNCATE indexer_cursors, indexed_blocks, raw_events, event_ccaf_auction_created, event_cca_checkpoint_updated, watched_contracts")
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// ---------- Cursor tests ----------

func TestCursor_Get_ReturnsZeroWhenEmpty(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	block, hash, err := s.CursorRepo().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if block != 0 {
		t.Errorf("expected block 0, got %d", block)
	}
	if hash != (common.Hash{}) {
		t.Errorf("expected empty hash, got %q", hash)
	}
}

func TestCursor_Upsert_ThenGet(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	err := s.CursorRepo().Upsert(ctx, 1, 100, common.HexToHash("0xabc"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	block, hash, err := s.CursorRepo().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if block != 100 {
		t.Errorf("expected block 100, got %d", block)
	}
	if hash != common.HexToHash("0xabc") {
		t.Errorf("expected hash 0xabc, got %q", hash)
	}
}

func TestCursor_Upsert_UpdatesExisting(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.CursorRepo().Upsert(ctx, 1, 100, common.HexToHash("0xabc"))
	_ = s.CursorRepo().Upsert(ctx, 1, 200, common.HexToHash("0xdef"))

	block, hash, err := s.CursorRepo().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if block != 200 {
		t.Errorf("expected block 200, got %d", block)
	}
	if hash != common.HexToHash("0xdef") {
		t.Errorf("expected hash 0xdef, got %q", hash)
	}
}

func TestCursor_ScopedByChainID(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.CursorRepo().Upsert(ctx, 1, 100, common.HexToHash("0xaaa"))
	_ = s.CursorRepo().Upsert(ctx, 2, 200, common.HexToHash("0xbbb"))

	block1, hash1, _ := s.CursorRepo().Get(ctx, 1)
	block2, hash2, _ := s.CursorRepo().Get(ctx, 2)

	if block1 != 100 || hash1 != common.HexToHash("0xaaa") {
		t.Errorf("chain 1: got block=%d hash=%q", block1, hash1)
	}
	if block2 != 200 || hash2 != common.HexToHash("0xbbb") {
		t.Errorf("chain 2: got block=%d hash=%q", block2, hash2)
	}
}

// ---------- Block tests ----------

func TestBlock_Insert_GetHash_RoundTrip(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	err := s.BlockRepo().Insert(ctx, 1, 10, common.HexToHash("0xblockhash"), common.HexToHash("0xparenthash"))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	hash, err := s.BlockRepo().GetHash(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetHash: %v", err)
	}
	if hash != common.HexToHash("0xblockhash") {
		t.Errorf("expected 0xblockhash, got %q", hash)
	}
}

func TestBlock_Insert_DuplicateNoError(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.BlockRepo().Insert(ctx, 1, 10, common.HexToHash("0xblockhash"), common.HexToHash("0xparenthash"))
	err := s.BlockRepo().Insert(ctx, 1, 10, common.HexToHash("0xblockhash"), common.HexToHash("0xparenthash"))
	if err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}
}

func TestBlock_GetHash_NonExistent(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	hash, err := s.BlockRepo().GetHash(ctx, 1, 999)
	if err != nil {
		t.Fatalf("GetHash: %v", err)
	}
	if hash != (common.Hash{}) {
		t.Errorf("expected empty hash, got %q", hash)
	}
}

func TestBlock_DeleteFrom_RemovesGTE(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.BlockRepo().Insert(ctx, 1, 10, common.HexToHash("0xa"), common.HexToHash("0xp"))
	_ = s.BlockRepo().Insert(ctx, 1, 11, common.HexToHash("0xb"), common.HexToHash("0xp"))
	_ = s.BlockRepo().Insert(ctx, 1, 12, common.HexToHash("0xc"), common.HexToHash("0xp"))

	err := s.BlockRepo().DeleteFrom(ctx, 1, 11)
	if err != nil {
		t.Fatalf("DeleteFrom: %v", err)
	}

	// block 11 and 12 should be gone
	hash11, _ := s.BlockRepo().GetHash(ctx, 1, 11)
	hash12, _ := s.BlockRepo().GetHash(ctx, 1, 12)
	if hash11 != (common.Hash{}) || hash12 != (common.Hash{}) {
		t.Errorf("blocks >= 11 should be deleted")
	}
}

func TestBlock_DeleteFrom_KeepsLT(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.BlockRepo().Insert(ctx, 1, 10, common.HexToHash("0xa"), common.HexToHash("0xp"))
	_ = s.BlockRepo().Insert(ctx, 1, 11, common.HexToHash("0xb"), common.HexToHash("0xp"))

	_ = s.BlockRepo().DeleteFrom(ctx, 1, 11)

	hash, _ := s.BlockRepo().GetHash(ctx, 1, 10)
	if hash != common.HexToHash("0xa") {
		t.Errorf("block 10 should still exist, got hash=%q", hash)
	}
}

// ---------- Raw event tests ----------

func makeRawEvent(chainID int64, blockNumber uint64, logIndex uint) *cca.RawEvent {
	return &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: blockNumber,
		BlockHash:   common.HexToHash("0xblockhash"),
		TxHash:      common.HexToHash("0xtxhash"),
		LogIndex:    logIndex,
		Address:     common.HexToAddress("0x1234"),
		EventName:   "Transfer",
		TopicsJSON:  `["0xtopic1"]`,
		DataHex:     "0xdata",
		DecodedJSON: `{"key":"value"}`,
	}
}

func TestRawEvent_Insert(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	ev := makeRawEvent(1, 100, 0)
	err := s.RawEventRepo().Insert(ctx, ev)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Verify via direct query
	ps := s.(*pgStore)
	var count int
	_ = ps.pool.QueryRow(ctx, "SELECT COUNT(*) FROM raw_events WHERE chain_id = 1 AND block_number = 100").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestRawEvent_Insert_DuplicateNoError(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	ev := makeRawEvent(1, 100, 0)
	_ = s.RawEventRepo().Insert(ctx, ev)
	err := s.RawEventRepo().Insert(ctx, ev)
	if err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}
}

func TestRawEvent_DeleteFromBlock_RemovesGTE(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(1, 10, 0))
	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(1, 11, 0))
	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(1, 12, 0))

	err := s.RawEventRepo().DeleteFromBlock(ctx, 1, 11)
	if err != nil {
		t.Fatalf("DeleteFromBlock: %v", err)
	}

	ps := s.(*pgStore)
	var count int
	_ = ps.pool.QueryRow(ctx, "SELECT COUNT(*) FROM raw_events WHERE chain_id = 1 AND block_number >= 11").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 rows >= 11, got %d", count)
	}
}

func TestRawEvent_DeleteFromBlock_KeepsLT(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(1, 10, 0))
	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(1, 11, 0))

	_ = s.RawEventRepo().DeleteFromBlock(ctx, 1, 11)

	ps := s.(*pgStore)
	var count int
	_ = ps.pool.QueryRow(ctx, "SELECT COUNT(*) FROM raw_events WHERE chain_id = 1 AND block_number = 10").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row at block 10, got %d", count)
	}
}

// ---------- Auction tests ----------

func makeAuction(chainID int64, blockNumber uint64) *cca.Auction {
	return &cca.Auction{
		AuctionAddress:         common.HexToAddress("0xAuction1"),
		Token:                  common.HexToAddress("0xToken1"),
		Amount:                 big.NewInt(1000000),
		Currency:               common.HexToAddress("0xCurrency1"),
		TokensRecipient:        common.HexToAddress("0xTokensRecipient1"),
		FundsRecipient:         common.HexToAddress("0xFundsRecipient1"),
		StartBlock:             100,
		EndBlock:               200,
		ClaimBlock:             300,
		TickSpacing:            big.NewInt(10),
		ValidationHook:         common.HexToAddress("0xValidationHook1"),
		FloorPrice:             big.NewInt(500),
		RequiredCurrencyRaised: big.NewInt(999999999999),
		ChainID:                chainID,
		BlockNumber:            blockNumber,
		TxHash:                 common.HexToHash("0xauctionTxHash"),
		LogIndex:               3,
	}
}

func TestAuction_Insert_GetByAddress_RoundTrip(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	a := makeAuction(1, 50)
	err := s.AuctionRepo().Insert(ctx, a)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	addr := lowerHex(a.AuctionAddress)
	got, err := s.AuctionRepo().GetByAddress(ctx, 1, addr)
	if err != nil {
		t.Fatalf("GetByAddress: %v", err)
	}
	if got == nil {
		t.Fatal("expected auction, got nil")
	}

	if got.AuctionAddress != a.AuctionAddress {
		t.Errorf("AuctionAddress: got %s, want %s", got.AuctionAddress.Hex(), a.AuctionAddress.Hex())
	}
	if got.Token != a.Token {
		t.Errorf("Token: got %s, want %s", got.Token.Hex(), a.Token.Hex())
	}
	if got.Amount.Cmp(a.Amount) != 0 {
		t.Errorf("Amount: got %s, want %s", got.Amount.String(), a.Amount.String())
	}
	if got.Currency != a.Currency {
		t.Errorf("Currency mismatch")
	}
	if got.TokensRecipient != a.TokensRecipient {
		t.Errorf("TokensRecipient mismatch")
	}
	if got.FundsRecipient != a.FundsRecipient {
		t.Errorf("FundsRecipient mismatch")
	}
	if got.StartBlock != a.StartBlock {
		t.Errorf("StartBlock: got %d, want %d", got.StartBlock, a.StartBlock)
	}
	if got.EndBlock != a.EndBlock {
		t.Errorf("EndBlock: got %d, want %d", got.EndBlock, a.EndBlock)
	}
	if got.ClaimBlock != a.ClaimBlock {
		t.Errorf("ClaimBlock: got %d, want %d", got.ClaimBlock, a.ClaimBlock)
	}
	if got.TickSpacing.Cmp(a.TickSpacing) != 0 {
		t.Errorf("TickSpacing: got %s, want %s", got.TickSpacing.String(), a.TickSpacing.String())
	}
	if got.ValidationHook != a.ValidationHook {
		t.Errorf("ValidationHook mismatch")
	}
	if got.FloorPrice.Cmp(a.FloorPrice) != 0 {
		t.Errorf("FloorPrice: got %s, want %s", got.FloorPrice.String(), a.FloorPrice.String())
	}
	if got.RequiredCurrencyRaised.Cmp(a.RequiredCurrencyRaised) != 0 {
		t.Errorf("RequiredCurrencyRaised: got %s, want %s", got.RequiredCurrencyRaised.String(), a.RequiredCurrencyRaised.String())
	}
	if got.ChainID != a.ChainID {
		t.Errorf("ChainID: got %d, want %d", got.ChainID, a.ChainID)
	}
	if got.BlockNumber != a.BlockNumber {
		t.Errorf("BlockNumber: got %d, want %d", got.BlockNumber, a.BlockNumber)
	}
	if got.TxHash != a.TxHash {
		t.Errorf("TxHash: got %s, want %s", got.TxHash.Hex(), a.TxHash.Hex())
	}
	if got.LogIndex != a.LogIndex {
		t.Errorf("LogIndex: got %d, want %d", got.LogIndex, a.LogIndex)
	}
}

func TestAuction_Insert_DuplicateNoError(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	a := makeAuction(1, 50)
	_ = s.AuctionRepo().Insert(ctx, a)
	err := s.AuctionRepo().Insert(ctx, a)
	if err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}
}

func TestAuction_GetByAddress_NonExistent(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	got, err := s.AuctionRepo().GetByAddress(ctx, 1, "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetByAddress: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestAuction_DeleteFromBlock_RemovesGTE(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	a1 := makeAuction(1, 10)
	a1.AuctionAddress = common.HexToAddress("0xA1")
	a2 := makeAuction(1, 11)
	a2.AuctionAddress = common.HexToAddress("0xA2")
	a3 := makeAuction(1, 12)
	a3.AuctionAddress = common.HexToAddress("0xA3")

	_ = s.AuctionRepo().Insert(ctx, a1)
	_ = s.AuctionRepo().Insert(ctx, a2)
	_ = s.AuctionRepo().Insert(ctx, a3)

	err := s.AuctionRepo().DeleteFromBlock(ctx, 1, 11)
	if err != nil {
		t.Fatalf("DeleteFromBlock: %v", err)
	}

	// a1 should remain
	got1, _ := s.AuctionRepo().GetByAddress(ctx, 1, "0x00000000000000000000000000000000000000a1")
	if got1 == nil {
		t.Error("a1 at block 10 should still exist")
	}
	// a2 and a3 should be gone
	got2, _ := s.AuctionRepo().GetByAddress(ctx, 1, "0x00000000000000000000000000000000000000a2")
	got3, _ := s.AuctionRepo().GetByAddress(ctx, 1, "0x00000000000000000000000000000000000000a3")
	if got2 != nil || got3 != nil {
		t.Error("auctions at blocks >= 11 should be deleted")
	}
}

func TestAuction_BigIntFieldsRoundTrip(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	a := makeAuction(1, 50)
	// Use very large numbers
	a.Amount, _ = new(big.Int).SetString("123456789012345678901234567890", 10)
	a.TickSpacing = big.NewInt(42)
	a.FloorPrice, _ = new(big.Int).SetString("999999999999999999999999999999", 10)
	a.RequiredCurrencyRaised, _ = new(big.Int).SetString("1", 10)

	err := s.AuctionRepo().Insert(ctx, a)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	addr := lowerHex(a.AuctionAddress)
	got, err := s.AuctionRepo().GetByAddress(ctx, 1, addr)
	if err != nil {
		t.Fatalf("GetByAddress: %v", err)
	}
	if got == nil {
		t.Fatal("expected auction, got nil")
	}

	if got.Amount.Cmp(a.Amount) != 0 {
		t.Errorf("Amount: got %s, want %s", got.Amount.String(), a.Amount.String())
	}
	if got.FloorPrice.Cmp(a.FloorPrice) != 0 {
		t.Errorf("FloorPrice: got %s, want %s", got.FloorPrice.String(), a.FloorPrice.String())
	}
	if got.RequiredCurrencyRaised.Cmp(a.RequiredCurrencyRaised) != 0 {
		t.Errorf("RequiredCurrencyRaised: got %s, want %s", got.RequiredCurrencyRaised.String(), a.RequiredCurrencyRaised.String())
	}
}

// ---------- WithTx integration tests ----------

func TestWithTx_CommitMakesWritesVisible(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	err := s.WithTx(ctx, func(txStore store.Store) error {
		return txStore.CursorRepo().Upsert(ctx, 77, 500, common.HexToHash("0xtxhash"))
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	block, hash, err := s.CursorRepo().Get(ctx, 77)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if block != 500 || hash != common.HexToHash("0xtxhash") {
		t.Errorf("expected (500, 0xtxhash), got (%d, %q)", block, hash)
	}
}

func TestWithTx_RollbackDiscardsWrites(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	_ = s.WithTx(ctx, func(txStore store.Store) error {
		_ = txStore.CursorRepo().Upsert(ctx, 88, 600, common.HexToHash("0xrollback"))
		return fmt.Errorf("force rollback")
	})

	block, hash, err := s.CursorRepo().Get(ctx, 88)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if block != 0 || hash != (common.Hash{}) {
		t.Errorf("expected (0, empty) after rollback, got (%d, %q)", block, hash)
	}
}

// ---------- WatchedContract helpers ----------

// makeWatchedContract builds a WatchedContract with sensible defaults.
// callers can override fields after creation.
func makeWatchedContract(chainID int64, addr common.Address, startBlock, lastIndexed uint64) *cca.WatchedContract {
	return &cca.WatchedContract{
		ChainID:          chainID,
		Address:          addr,
		Label:            "test-contract",
		StartBlock:       startBlock,
		StartBlockTime:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		LastIndexedBlock: lastIndexed,
	}
}

// ---------- WatchedContract tests ----------

// TestWatchedContract_Insert_StoresContract verifies that inserting a watched
// contract persists all key fields. We read it back via ListNeedingBackfill
// (with a cursor above the contract's last_indexed_block) to confirm the
// round-trip.
func TestWatchedContract_Insert_StoresContract(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	wantChainID := int64(1)
	wantAddr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	wantLabel := "auction-factory"
	wantStartBlock := uint64(100)
	wantLastIndexed := uint64(50)

	wc := makeWatchedContract(wantChainID, wantAddr, wantStartBlock, wantLastIndexed)
	wc.Label = wantLabel

	err := s.WatchedContractRepo().Insert(ctx, wc)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Use a globalCursor above last_indexed_block so the contract appears in
	// the "needing backfill" set.
	globalCursor := uint64(51)
	contracts, err := s.WatchedContractRepo().ListNeedingBackfill(ctx, wantChainID, globalCursor)
	if err != nil {
		t.Fatalf("ListNeedingBackfill: %v", err)
	}

	wantCount := 1
	if len(contracts) != wantCount {
		t.Fatalf("expected %d contract(s), got %d", wantCount, len(contracts))
	}

	got := contracts[0]
	if got.ChainID != wantChainID {
		t.Errorf("ChainID: got %d, want %d", got.ChainID, wantChainID)
	}
	if got.Address != wantAddr {
		t.Errorf("Address: got %s, want %s", got.Address.Hex(), wantAddr.Hex())
	}
	if got.Label != wantLabel {
		t.Errorf("Label: got %q, want %q", got.Label, wantLabel)
	}
	if got.StartBlock != wantStartBlock {
		t.Errorf("StartBlock: got %d, want %d", got.StartBlock, wantStartBlock)
	}
	if got.LastIndexedBlock != wantLastIndexed {
		t.Errorf("LastIndexedBlock: got %d, want %d", got.LastIndexedBlock, wantLastIndexed)
	}
}

// TestWatchedContract_Insert_IdempotentOnConflict verifies that inserting the
// same (chain_id, address) pair twice does not return an error and does not
// create a duplicate row. The ON CONFLICT DO NOTHING clause should silently
// discard the second write.
func TestWatchedContract_Insert_IdempotentOnConflict(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	addr := common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	wc := makeWatchedContract(1, addr, 10, 0)

	// First insert should succeed.
	if err := s.WatchedContractRepo().Insert(ctx, wc); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	// Second insert of the same contract should also succeed (no error).
	if err := s.WatchedContractRepo().Insert(ctx, wc); err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}

	// Verify exactly one row exists by querying directly.
	ps := s.(*pgStore)
	var count int
	wantCount := 1
	err := ps.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM watched_contracts WHERE chain_id = $1 AND address = $2",
		int64(1), lowerHex(addr),
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != wantCount {
		t.Errorf("expected %d row(s), got %d", wantCount, count)
	}
}

// TestWatchedContract_ListCaughtUp_ReturnsOnlyCaughtUp inserts three contracts
// with different last_indexed_block values and verifies that ListCaughtUp only
// returns those whose cursor is at or above the supplied globalCursor.
func TestWatchedContract_ListCaughtUp_ReturnsOnlyCaughtUp(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	// Contract A: last_indexed_block = 100 (behind cursor)
	addrA := common.HexToAddress("0x0000000000000000000000000000000000000AAA")
	// Contract B: last_indexed_block = 200 (exactly at cursor)
	addrB := common.HexToAddress("0x0000000000000000000000000000000000000BBB")
	// Contract C: last_indexed_block = 300 (ahead of cursor)
	addrC := common.HexToAddress("0x0000000000000000000000000000000000000CCC")

	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrA, 10, 100))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrB, 20, 200))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrC, 30, 300))

	// globalCursor = 200: contracts B (==200) and C (>200) should be caught up.
	globalCursor := uint64(200)
	addrs, err := s.WatchedContractRepo().ListCaughtUp(ctx, chainID, globalCursor)
	if err != nil {
		t.Fatalf("ListCaughtUp: %v", err)
	}

	wantCount := 2
	if len(addrs) != wantCount {
		t.Fatalf("expected %d caught-up addresses, got %d", wantCount, len(addrs))
	}

	// Build a set of returned addresses for easier assertion.
	got := make(map[common.Address]bool)
	for _, a := range addrs {
		got[a] = true
	}

	// Contract A should NOT be in the result (100 < 200).
	if got[addrA] {
		t.Errorf("contract A (last_indexed_block=100) should not be caught up at cursor 200")
	}
	// Contract B should be in the result (200 >= 200).
	if !got[addrB] {
		t.Errorf("contract B (last_indexed_block=200) should be caught up at cursor 200")
	}
	// Contract C should be in the result (300 >= 200).
	if !got[addrC] {
		t.Errorf("contract C (last_indexed_block=300) should be caught up at cursor 200")
	}
}

// TestWatchedContract_ListNeedingBackfill_ReturnsOnlyBehind inserts contracts
// at different cursor positions and verifies ListNeedingBackfill returns only
// those behind the globalCursor, ordered by start_block ASC.
func TestWatchedContract_ListNeedingBackfill_ReturnsOnlyBehind(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	// Contract A: start_block=30, last_indexed_block=100 (behind cursor)
	addrA := common.HexToAddress("0x0000000000000000000000000000000000000AAA")
	// Contract B: start_block=10, last_indexed_block=50 (behind cursor, earlier start)
	addrB := common.HexToAddress("0x0000000000000000000000000000000000000BBB")
	// Contract C: start_block=20, last_indexed_block=200 (caught up, should be excluded)
	addrC := common.HexToAddress("0x0000000000000000000000000000000000000CCC")

	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrA, 30, 100))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrB, 10, 50))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrC, 20, 200))

	// globalCursor = 200: A (100 < 200) and B (50 < 200) need backfill.
	globalCursor := uint64(200)
	contracts, err := s.WatchedContractRepo().ListNeedingBackfill(ctx, chainID, globalCursor)
	if err != nil {
		t.Fatalf("ListNeedingBackfill: %v", err)
	}

	wantCount := 2
	if len(contracts) != wantCount {
		t.Fatalf("expected %d contracts, got %d", wantCount, len(contracts))
	}

	// Results should be ordered by start_block ASC: B (start=10) before A (start=30).
	wantFirstAddr := addrB
	wantSecondAddr := addrA
	if contracts[0].Address != wantFirstAddr {
		t.Errorf("first contract: got address %s, want %s", contracts[0].Address.Hex(), wantFirstAddr.Hex())
	}
	if contracts[1].Address != wantSecondAddr {
		t.Errorf("second contract: got address %s, want %s", contracts[1].Address.Hex(), wantSecondAddr.Hex())
	}

	// Contract C (last_indexed_block=200, which is NOT < 200) must not appear.
	for _, c := range contracts {
		if c.Address == addrC {
			t.Errorf("contract C (last_indexed_block=200) should not need backfill at cursor 200")
		}
	}
}

// TestWatchedContract_UpdateLastIndexedBlock_AdvancesCursor verifies that
// calling UpdateLastIndexedBlock moves a contract's cursor forward. After the
// update the contract should appear in the caught-up set for the new cursor
// value.
func TestWatchedContract_UpdateLastIndexedBlock_AdvancesCursor(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	addr := common.HexToAddress("0x0000000000000000000000000000000000001111")
	initialCursor := uint64(50)
	wc := makeWatchedContract(chainID, addr, 10, initialCursor)
	_ = s.WatchedContractRepo().Insert(ctx, wc)

	// Advance the cursor to 200.
	newCursor := uint64(200)
	err := s.WatchedContractRepo().UpdateLastIndexedBlock(ctx, chainID, addr, newCursor)
	if err != nil {
		t.Fatalf("UpdateLastIndexedBlock: %v", err)
	}

	// At globalCursor=200, the contract should now be caught up (last_indexed_block >= 200).
	addrs, err := s.WatchedContractRepo().ListCaughtUp(ctx, chainID, newCursor)
	if err != nil {
		t.Fatalf("ListCaughtUp: %v", err)
	}

	wantCount := 1
	if len(addrs) != wantCount {
		t.Fatalf("expected %d caught-up address(es), got %d", wantCount, len(addrs))
	}
	if addrs[0] != addr {
		t.Errorf("caught-up address: got %s, want %s", addrs[0].Hex(), addr.Hex())
	}
}

// TestWatchedContract_RollbackCursors_ResetsAffectedOnly inserts three
// contracts at varying cursor positions and calls RollbackCursors. Only
// contracts whose last_indexed_block >= fromBlock should be reset to
// fromBlock - 1; the rest must remain unchanged.
func TestWatchedContract_RollbackCursors_ResetsAffectedOnly(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	// Contract A: cursor at 50 (below fromBlock, should NOT be affected)
	addrA := common.HexToAddress("0x000000000000000000000000000000000000AAAA")
	// Contract B: cursor at 100 (exactly at fromBlock, should be rolled back)
	addrB := common.HexToAddress("0x000000000000000000000000000000000000BBBB")
	// Contract C: cursor at 200 (above fromBlock, should be rolled back)
	addrC := common.HexToAddress("0x000000000000000000000000000000000000CCCC")

	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrA, 1, 50))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrB, 1, 100))
	_ = s.WatchedContractRepo().Insert(ctx, makeWatchedContract(chainID, addrC, 1, 200))

	fromBlock := uint64(100)
	wantRolledBackCursor := fromBlock - 1 // 99

	err := s.WatchedContractRepo().RollbackCursors(ctx, chainID, fromBlock)
	if err != nil {
		t.Fatalf("RollbackCursors: %v", err)
	}

	// Contract A should still be at cursor 50 — check it is NOT caught up at 100
	// but IS caught up at 50.
	addrsAt50, _ := s.WatchedContractRepo().ListCaughtUp(ctx, chainID, 50)
	gotA := false
	for _, a := range addrsAt50 {
		if a == addrA {
			gotA = true
		}
	}
	if !gotA {
		t.Errorf("contract A should still be caught up at cursor 50 (unaffected by rollback)")
	}

	// Contracts B and C should now have last_indexed_block = 99.
	// At globalCursor = 100 they should need backfill.
	needBackfill, _ := s.WatchedContractRepo().ListNeedingBackfill(ctx, chainID, fromBlock)
	wantBackfillCount := 2
	if len(needBackfill) != wantBackfillCount {
		t.Fatalf("expected %d contracts needing backfill, got %d", wantBackfillCount, len(needBackfill))
	}
	for _, c := range needBackfill {
		if c.LastIndexedBlock != wantRolledBackCursor {
			t.Errorf("contract %s: last_indexed_block = %d, want %d",
				c.Address.Hex(), c.LastIndexedBlock, wantRolledBackCursor)
		}
	}
}

// TestRollbackFromBlock_IncludesWatchedContractCursors tests the pgStore-level
// RollbackFromBlock method end-to-end, verifying that it rolls back watched
// contract cursors in addition to deleting raw events and blocks. This is a
// broader integration test than RollbackCursors alone.
func TestRollbackFromBlock_IncludesWatchedContractCursors(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	addr := common.HexToAddress("0x0000000000000000000000000000000000005555")
	originalCursor := uint64(500)
	wc := makeWatchedContract(chainID, addr, 1, originalCursor)
	_ = s.WatchedContractRepo().Insert(ctx, wc)

	// Also insert a block and raw event at the rollback point to confirm
	// those are cleaned up too (ensures RollbackFromBlock is holistic).
	fromBlock := uint64(300)
	_ = s.BlockRepo().Insert(ctx, chainID, fromBlock, common.HexToHash("0xdeadbeef"), common.HexToHash("0xparent"))
	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(chainID, fromBlock, 0))

	ps := s.(*pgStore)
	err := ps.RollbackFromBlock(ctx, chainID, fromBlock)
	if err != nil {
		t.Fatalf("RollbackFromBlock: %v", err)
	}

	// The watched contract cursor should be rolled back to fromBlock - 1.
	wantCursor := fromBlock - 1 // 299
	contracts, err := s.WatchedContractRepo().ListNeedingBackfill(ctx, chainID, fromBlock)
	if err != nil {
		t.Fatalf("ListNeedingBackfill: %v", err)
	}

	wantCount := 1
	if len(contracts) != wantCount {
		t.Fatalf("expected %d contract(s) needing backfill, got %d", wantCount, len(contracts))
	}
	if contracts[0].LastIndexedBlock != wantCursor {
		t.Errorf("LastIndexedBlock: got %d, want %d", contracts[0].LastIndexedBlock, wantCursor)
	}

	// Confirm block was also deleted.
	hash, _ := s.BlockRepo().GetHash(ctx, chainID, fromBlock)
	wantHash := common.Hash{}
	if hash != wantHash {
		t.Errorf("block at fromBlock should be deleted, got hash %s", hash.Hex())
	}

	// Confirm raw event was also deleted.
	var eventCount int
	_ = ps.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM raw_events WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	).Scan(&eventCount)
	wantEventCount := 0
	if eventCount != wantEventCount {
		t.Errorf("expected %d raw events at fromBlock, got %d", wantEventCount, eventCount)
	}
}

// ---------- Checkpoint helpers ----------

// makeCheckpoint builds a Checkpoint with sensible defaults for the given
// chain, auction address, logical block number, and tx block number.
// Callers can override fields after creation.
func makeCheckpoint(chainID int64, auctionAddr common.Address, blockNumber, txBlockNumber uint64, logIndex uint) *cca.Checkpoint {
	return &cca.Checkpoint{
		BlockNumber:      blockNumber,
		ClearingPriceQ96: parseBigInt("79228162514264337593543950336"), // ~1.0 in Q96
		CumulativeMps:    42,
		AuctionAddress:   auctionAddr,
		ChainID:          chainID,
		TxBlockNumber:    txBlockNumber,
		TxBlockTime:      time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		TxHash:           common.HexToHash("0xcheckpointTxHash"),
		LogIndex:         logIndex,
	}
}

// ---------- Checkpoint tests ----------

// TestCheckpoint_Insert_GetLatest_RoundTrip verifies that inserting a
// checkpoint and then calling GetLatest returns the checkpoint with the
// highest block_number for that auction. This covers the basic write-then-read
// round-trip and ensures all fields survive the persistence layer.
func TestCheckpoint_Insert_GetLatest_RoundTrip(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA01")

	// Insert two checkpoints at different logical block numbers.
	// GetLatest should return the one with the higher block_number.
	cp1 := makeCheckpoint(chainID, auctionAddr, 10, 1000, 0)
	cp2 := makeCheckpoint(chainID, auctionAddr, 20, 2000, 1)
	cp2.ClearingPriceQ96 = parseBigInt("158456325028528675187087900672") // ~2.0 in Q96
	cp2.CumulativeMps = 84
	cp2.TxHash = common.HexToHash("0xcheckpointTxHash2")
	cp2.TxBlockTime = time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)

	err := s.CheckpointRepo().Insert(ctx, cp1)
	if err != nil {
		t.Fatalf("Insert cp1: %v", err)
	}
	err = s.CheckpointRepo().Insert(ctx, cp2)
	if err != nil {
		t.Fatalf("Insert cp2: %v", err)
	}

	// GetLatest should return cp2 (block_number=20 > block_number=10).
	addr := lowerHex(auctionAddr)
	got, err := s.CheckpointRepo().GetLatest(ctx, chainID, addr)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got == nil {
		t.Fatal("expected checkpoint, got nil")
	}

	wantBlockNumber := uint64(20)
	if got.BlockNumber != wantBlockNumber {
		t.Errorf("BlockNumber: got %d, want %d", got.BlockNumber, wantBlockNumber)
	}

	wantClearingPrice := parseBigInt("158456325028528675187087900672")
	if got.ClearingPriceQ96.Cmp(wantClearingPrice) != 0 {
		t.Errorf("ClearingPriceQ96: got %s, want %s", got.ClearingPriceQ96.String(), wantClearingPrice.String())
	}

	wantCumulativeMps := uint32(84)
	if got.CumulativeMps != wantCumulativeMps {
		t.Errorf("CumulativeMps: got %d, want %d", got.CumulativeMps, wantCumulativeMps)
	}

	wantAuctionAddr := auctionAddr
	if got.AuctionAddress != wantAuctionAddr {
		t.Errorf("AuctionAddress: got %s, want %s", got.AuctionAddress.Hex(), wantAuctionAddr.Hex())
	}

	wantChainID := chainID
	if got.ChainID != wantChainID {
		t.Errorf("ChainID: got %d, want %d", got.ChainID, wantChainID)
	}

	wantTxBlockNumber := uint64(2000)
	if got.TxBlockNumber != wantTxBlockNumber {
		t.Errorf("TxBlockNumber: got %d, want %d", got.TxBlockNumber, wantTxBlockNumber)
	}

	wantTxBlockTime := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
	if !got.TxBlockTime.Equal(wantTxBlockTime) {
		t.Errorf("TxBlockTime: got %v, want %v", got.TxBlockTime, wantTxBlockTime)
	}

	wantTxHash := common.HexToHash("0xcheckpointTxHash2")
	if got.TxHash != wantTxHash {
		t.Errorf("TxHash: got %s, want %s", got.TxHash.Hex(), wantTxHash.Hex())
	}

	wantLogIndex := uint(1)
	if got.LogIndex != wantLogIndex {
		t.Errorf("LogIndex: got %d, want %d", got.LogIndex, wantLogIndex)
	}
}

// TestCheckpoint_GetLatest_NonExistent verifies that GetLatest returns nil
// when no checkpoints exist for the given auction, rather than returning
// an error.
func TestCheckpoint_GetLatest_NonExistent(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	got, err := s.CheckpointRepo().GetLatest(ctx, 1, "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent auction, got %+v", got)
	}
}

// TestCheckpoint_Insert_DuplicateNoError verifies that inserting the same
// checkpoint twice (same chain_id, auction_address, block_number PK) does not
// return an error, matching the ON CONFLICT DO NOTHING pattern used elsewhere.
func TestCheckpoint_Insert_DuplicateNoError(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	cp := makeCheckpoint(1, common.HexToAddress("0xBBBB"), 10, 1000, 0)
	_ = s.CheckpointRepo().Insert(ctx, cp)
	err := s.CheckpointRepo().Insert(ctx, cp)
	if err != nil {
		t.Fatalf("duplicate Insert should not error: %v", err)
	}
}

// TestCheckpoint_ListByAuction_DescendingWithPagination tests that
// ListByAuction returns checkpoints in descending block_number order and
// supports cursor-based pagination. Covers: first page without cursor, second
// page with cursor, and page size limit.
func TestCheckpoint_ListByAuction_DescendingWithPagination(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC01")
	addr := lowerHex(auctionAddr)

	// Insert 5 checkpoints at block numbers 10, 20, 30, 40, 50.
	for i := uint64(1); i <= 5; i++ {
		blockNum := i * 10
		txBlockNum := uint64(1000) + i
		cp := makeCheckpoint(chainID, auctionAddr, blockNum, txBlockNum, uint(i))
		err := s.CheckpointRepo().Insert(ctx, cp)
		if err != nil {
			t.Fatalf("Insert block %d: %v", blockNum, err)
		}
	}

	// --- First page: fetch 3 items, no cursor ---
	// Should return block_numbers [50, 40, 30] in descending order.
	firstPageParams := store.PaginationParams{Limit: 3}
	page1, err := s.CheckpointRepo().ListByAuction(ctx, chainID, addr, firstPageParams)
	if err != nil {
		t.Fatalf("ListByAuction page 1: %v", err)
	}

	wantPage1Len := 3
	if len(page1) != wantPage1Len {
		t.Fatalf("page 1: expected %d results, got %d", wantPage1Len, len(page1))
	}

	wantPage1Blocks := []uint64{50, 40, 30}
	for i, want := range wantPage1Blocks {
		if page1[i].BlockNumber != want {
			t.Errorf("page 1[%d]: got block_number %d, want %d", i, page1[i].BlockNumber, want)
		}
	}

	// --- Second page: use cursor from last item of page 1 ---
	// Cursor at (block_number=30, log_index=3); next page should return [20, 10].
	cursorBlock := page1[len(page1)-1].BlockNumber
	cursorLogIdx := page1[len(page1)-1].LogIndex
	secondPageParams := store.PaginationParams{
		Limit:             3,
		CursorBlockNumber: &cursorBlock,
		CursorLogIndex:    &cursorLogIdx,
	}
	page2, err := s.CheckpointRepo().ListByAuction(ctx, chainID, addr, secondPageParams)
	if err != nil {
		t.Fatalf("ListByAuction page 2: %v", err)
	}

	wantPage2Len := 2
	if len(page2) != wantPage2Len {
		t.Fatalf("page 2: expected %d results, got %d", wantPage2Len, len(page2))
	}

	wantPage2Blocks := []uint64{20, 10}
	for i, want := range wantPage2Blocks {
		if page2[i].BlockNumber != want {
			t.Errorf("page 2[%d]: got block_number %d, want %d", i, page2[i].BlockNumber, want)
		}
	}
}

// TestCheckpoint_ListByAuction_EmptyResult verifies that ListByAuction
// returns an empty (non-nil or nil) slice with no error when no checkpoints
// exist for the requested auction.
func TestCheckpoint_ListByAuction_EmptyResult(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	params := store.PaginationParams{Limit: 10}
	got, err := s.CheckpointRepo().ListByAuction(ctx, 1, "0x0000000000000000000000000000000000000000", params)
	if err != nil {
		t.Fatalf("ListByAuction: %v", err)
	}

	wantLen := 0
	if len(got) != wantLen {
		t.Errorf("expected %d results for empty auction, got %d", wantLen, len(got))
	}
}

// TestCheckpoint_ListByAuction_ScopedByAuction verifies that ListByAuction
// only returns checkpoints for the specified auction address, not checkpoints
// belonging to other auctions on the same chain.
func TestCheckpoint_ListByAuction_ScopedByAuction(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionA := common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	auctionB := common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")

	// Insert one checkpoint for each auction.
	cpA := makeCheckpoint(chainID, auctionA, 10, 1000, 0)
	cpB := makeCheckpoint(chainID, auctionB, 20, 2000, 1)

	_ = s.CheckpointRepo().Insert(ctx, cpA)
	_ = s.CheckpointRepo().Insert(ctx, cpB)

	// Query only auction A.
	params := store.PaginationParams{Limit: 10}
	got, err := s.CheckpointRepo().ListByAuction(ctx, chainID, lowerHex(auctionA), params)
	if err != nil {
		t.Fatalf("ListByAuction: %v", err)
	}

	wantLen := 1
	if len(got) != wantLen {
		t.Fatalf("expected %d checkpoint(s) for auction A, got %d", wantLen, len(got))
	}

	wantBlockNumber := uint64(10)
	if got[0].BlockNumber != wantBlockNumber {
		t.Errorf("BlockNumber: got %d, want %d", got[0].BlockNumber, wantBlockNumber)
	}
}

// TestCheckpoint_DeleteFromBlock_DeletesByTxBlockNumber verifies that
// DeleteFromBlock removes checkpoints based on tx_block_number (the chain
// block where the tx was mined), NOT the logical block_number field. This
// distinction is critical for reorg handling: when we roll back chain blocks,
// we must delete events from those chain blocks regardless of their
// auction-internal block numbering.
func TestCheckpoint_DeleteFromBlock_DeletesByTxBlockNumber(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0xDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD01")
	addr := lowerHex(auctionAddr)

	// Insert three checkpoints with different tx_block_numbers.
	// Note: logical block_number != tx_block_number to prove we delete by tx_block.
	//   cp1: block_number=5,  tx_block_number=100 (should survive)
	//   cp2: block_number=10, tx_block_number=200 (should be deleted, tx_block >= 200)
	//   cp3: block_number=15, tx_block_number=300 (should be deleted, tx_block >= 200)
	cp1 := makeCheckpoint(chainID, auctionAddr, 5, 100, 0)
	cp2 := makeCheckpoint(chainID, auctionAddr, 10, 200, 1)
	cp3 := makeCheckpoint(chainID, auctionAddr, 15, 300, 2)

	_ = s.CheckpointRepo().Insert(ctx, cp1)
	_ = s.CheckpointRepo().Insert(ctx, cp2)
	_ = s.CheckpointRepo().Insert(ctx, cp3)

	// Delete from tx_block_number >= 200.
	fromBlock := uint64(200)
	err := s.CheckpointRepo().DeleteFromBlock(ctx, chainID, fromBlock)
	if err != nil {
		t.Fatalf("DeleteFromBlock: %v", err)
	}

	// Only cp1 (tx_block_number=100) should remain.
	params := store.PaginationParams{Limit: 10}
	remaining, err := s.CheckpointRepo().ListByAuction(ctx, chainID, addr, params)
	if err != nil {
		t.Fatalf("ListByAuction after delete: %v", err)
	}

	wantLen := 1
	if len(remaining) != wantLen {
		t.Fatalf("expected %d remaining checkpoint(s), got %d", wantLen, len(remaining))
	}

	wantBlockNumber := uint64(5)
	if remaining[0].BlockNumber != wantBlockNumber {
		t.Errorf("remaining checkpoint BlockNumber: got %d, want %d", remaining[0].BlockNumber, wantBlockNumber)
	}
}

// TestCheckpoint_DeleteFromBlock_KeepsBelowThreshold verifies that
// DeleteFromBlock does not remove checkpoints whose tx_block_number is
// strictly below the fromBlock threshold.
func TestCheckpoint_DeleteFromBlock_KeepsBelowThreshold(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0xEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE01")
	addr := lowerHex(auctionAddr)

	cp := makeCheckpoint(chainID, auctionAddr, 10, 99, 0)
	_ = s.CheckpointRepo().Insert(ctx, cp)

	// Delete from tx_block_number >= 100. The checkpoint at tx_block 99 should survive.
	err := s.CheckpointRepo().DeleteFromBlock(ctx, chainID, 100)
	if err != nil {
		t.Fatalf("DeleteFromBlock: %v", err)
	}

	params := store.PaginationParams{Limit: 10}
	remaining, err := s.CheckpointRepo().ListByAuction(ctx, chainID, addr, params)
	if err != nil {
		t.Fatalf("ListByAuction: %v", err)
	}

	wantLen := 1
	if len(remaining) != wantLen {
		t.Errorf("expected %d checkpoint(s) to survive, got %d", wantLen, len(remaining))
	}
}

// TestCheckpoint_BigIntClearingPriceRoundTrip verifies that very large
// ClearingPriceQ96 values (exceeding int64 range) survive the database
// round-trip without loss of precision.
func TestCheckpoint_BigIntClearingPriceRoundTrip(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF01")

	cp := makeCheckpoint(chainID, auctionAddr, 1, 1000, 0)
	// Use a value that exceeds int64 range to test NUMERIC handling.
	wantPrice, _ := new(big.Int).SetString("123456789012345678901234567890123456789", 10)
	cp.ClearingPriceQ96 = wantPrice

	err := s.CheckpointRepo().Insert(ctx, cp)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.CheckpointRepo().GetLatest(ctx, chainID, lowerHex(auctionAddr))
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got == nil {
		t.Fatal("expected checkpoint, got nil")
	}

	if got.ClearingPriceQ96.Cmp(wantPrice) != 0 {
		t.Errorf("ClearingPriceQ96: got %s, want %s", got.ClearingPriceQ96.String(), wantPrice.String())
	}
}

// TestRollbackFromBlock_IncludesCheckpoints verifies that the pgStore-level
// RollbackFromBlock method also deletes checkpoint records (by tx_block_number)
// in addition to raw events, auctions, blocks, and watched contract cursors.
// This is an end-to-end integration test ensuring the rollback cascade is
// complete for reorg recovery.
func TestRollbackFromBlock_IncludesCheckpoints(t *testing.T) {
	s := testStore(t)
	truncateAll(t, s)
	ctx := context.Background()

	chainID := int64(1)
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")

	// Insert a checkpoint at tx_block_number=300 (at the rollback point).
	cpAtRollback := makeCheckpoint(chainID, auctionAddr, 10, 300, 0)
	// Insert a checkpoint at tx_block_number=200 (below the rollback point, should survive).
	cpBelow := makeCheckpoint(chainID, auctionAddr, 5, 200, 1)

	err := s.CheckpointRepo().Insert(ctx, cpAtRollback)
	if err != nil {
		t.Fatalf("Insert checkpoint at rollback: %v", err)
	}
	err = s.CheckpointRepo().Insert(ctx, cpBelow)
	if err != nil {
		t.Fatalf("Insert checkpoint below rollback: %v", err)
	}

	// Also insert a block and raw event so we exercise the full cascade.
	fromBlock := uint64(300)
	_ = s.BlockRepo().Insert(ctx, chainID, fromBlock, common.HexToHash("0xrollbackblock"), common.HexToHash("0xparent"))
	_ = s.RawEventRepo().Insert(ctx, makeRawEvent(chainID, fromBlock, 0))

	ps := s.(*pgStore)
	err = ps.RollbackFromBlock(ctx, chainID, fromBlock)
	if err != nil {
		t.Fatalf("RollbackFromBlock: %v", err)
	}

	// The checkpoint at tx_block_number=300 should be deleted.
	// The checkpoint at tx_block_number=200 should survive.
	addr := lowerHex(auctionAddr)
	params := store.PaginationParams{Limit: 10}
	remaining, err := s.CheckpointRepo().ListByAuction(ctx, chainID, addr, params)
	if err != nil {
		t.Fatalf("ListByAuction after rollback: %v", err)
	}

	wantLen := 1
	if len(remaining) != wantLen {
		t.Fatalf("expected %d checkpoint(s) after rollback, got %d", wantLen, len(remaining))
	}

	wantSurvivingBlock := uint64(5)
	if remaining[0].BlockNumber != wantSurvivingBlock {
		t.Errorf("surviving checkpoint BlockNumber: got %d, want %d", remaining[0].BlockNumber, wantSurvivingBlock)
	}

	// Confirm block was also deleted (sanity check for cascade).
	hash, _ := s.BlockRepo().GetHash(ctx, chainID, fromBlock)
	wantHash := common.Hash{}
	if hash != wantHash {
		t.Errorf("block at fromBlock should be deleted, got hash %s", hash.Hex())
	}
}
