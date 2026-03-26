package postgres

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
)

func truncateAll(t *testing.T, s store.Store) {
	t.Helper()
	ps := s.(*pgStore)
	ctx := context.Background()
	_, err := ps.pool.Exec(ctx, "TRUNCATE indexer_cursors, indexed_blocks, raw_events, event_ccaf_auction_created")
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
