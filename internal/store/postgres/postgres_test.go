package postgres_test

// postgres_test.go contains integration tests for the PostgreSQL store
// implementation. These tests run against a real PostgreSQL instance via
// testcontainers — a Docker container is started before the test suite and
// torn down afterward.
//
// Why integration tests (not mocks)?
//   The store layer is thin SQL — the interesting behavior lives in the
//   database (upsert semantics, cascading deletes, type coercion). Mocking
//   the DB would test nothing useful. By running against real Postgres we
//   verify that the SQL is correct, the types are compatible, and the
//   transactional semantics work as expected.
//
// Test isolation:
//   Each test calls truncateAll() at the start to clear all tables, so tests
//   are independent and can run in any order. This is simpler than per-test
//   transactions because some tests (WithTx) need to control their own
//   transaction boundaries.
//
// Verification approach:
//   Tests insert data through the store interface and then verify it using
//   either:
//     (a) The same store interface (round-trip tests like Upsert→Get), or
//     (b) A separate pgxpool connection (testPool) with raw SQL queries.
//   Approach (b) avoids testing the read path with the write path and gives
//   us ground-truth verification of what's actually in the database.

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/cca/go-indexer/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

var (
	testStore   store.Store         // the store under test (uses the store interface, not concrete type)
	testPool    *pgxpool.Pool       // separate connection pool for raw SQL verification queries
	pgContainer testcontainers.Container // the Docker container running Postgres
)

// TestMain sets up the test environment before any tests run and tears it down
// after all tests complete. This is the standard Go pattern for expensive
// per-suite setup.
//
// Lifecycle:
//  1. Start a Postgres 16 container via testcontainers.
//  2. Create a store (which runs migrations, creating all tables).
//  3. Create a separate pgxpool for verification queries.
//  4. Run all tests.
//  5. Clean up: close pools, terminate the container.
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start a Postgres container. The wait strategy waits for the "ready to
	// accept connections" log message to appear twice — once for the initial
	// startup and once after Postgres restarts to apply configuration.
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %v", err)
	}
	pgContainer = container

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get connection string: %v", err)
	}

	// Create the store under test — this also runs migrations.
	s, err := postgres.New(ctx, connStr)
	if err != nil {
		log.Fatalf("failed to create store: %v", err)
	}
	testStore = s

	// Create a separate pool for verification queries. Using a different
	// connection ensures we're reading committed data, not seeing uncommitted
	// changes from the store's connection.
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("failed to create verification pool: %v", err)
	}
	testPool = pool

	code := m.Run()

	// Cleanup
	pool.Close()
	s.Close()
	if err := container.Terminate(ctx); err != nil {
		log.Printf("failed to terminate container: %v", err)
	}

	os.Exit(code)
}

// truncateAll removes all data from every table so each test starts with a
// clean slate. Using TRUNCATE (not DELETE) is faster and resets sequences.
func truncateAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, "TRUNCATE cursors, blocks, raw_events, auctions")
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// ===========================================================================
// CursorRepo tests
// ===========================================================================

// TestCursorRepo_GetReturnsZeroOnEmpty verifies that Get returns zero values
// when no cursor has been saved for a chain. This is the "first run" case —
// the indexer uses these zero values to know it needs to start from StartBlock.
func TestCursorRepo_GetReturnsZeroOnEmpty(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	blockNum, blockHash, err := testStore.CursorRepo().Get(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blockNum != 0 {
		t.Errorf("expected blockNumber 0, got %d", blockNum)
	}
	if blockHash != "" {
		t.Errorf("expected empty blockHash, got %q", blockHash)
	}
}

// TestCursorRepo_UpsertAndGetRoundTrip verifies that:
//  1. Upsert creates a new cursor row and Get retrieves it correctly.
//  2. A second Upsert overwrites the existing row (ON CONFLICT DO UPDATE).
//
// This is a round-trip test (write via store, read via store) that validates
// the upsert SQL and the scan logic together.
func TestCursorRepo_UpsertAndGetRoundTrip(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	chainID := int64(324)

	// First upsert — creates the row.
	if err := testStore.CursorRepo().Upsert(ctx, chainID, 100, "0xaaa"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	blockNum, blockHash, err := testStore.CursorRepo().Get(ctx, chainID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if blockNum != 100 {
		t.Errorf("expected blockNumber 100, got %d", blockNum)
	}
	if blockHash != "0xaaa" {
		t.Errorf("expected blockHash 0xaaa, got %q", blockHash)
	}

	// Second upsert — overwrites the existing row with new values.
	if err := testStore.CursorRepo().Upsert(ctx, chainID, 200, "0xbbb"); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	blockNum, blockHash, err = testStore.CursorRepo().Get(ctx, chainID)
	if err != nil {
		t.Fatalf("get after overwrite: %v", err)
	}
	if blockNum != 200 {
		t.Errorf("expected blockNumber 200, got %d", blockNum)
	}
	if blockHash != "0xbbb" {
		t.Errorf("expected blockHash 0xbbb, got %q", blockHash)
	}
}

// ===========================================================================
// BlockRepo tests
// ===========================================================================

// TestBlockRepo_InsertAndGetHash verifies that a block can be inserted and
// its hash retrieved by chain ID and block number.
func TestBlockRepo_InsertAndGetHash(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	chainID := int64(324)

	if err := testStore.BlockRepo().Insert(ctx, chainID, 10, "0xblockhash10", "0xparent10"); err != nil {
		t.Fatalf("insert block: %v", err)
	}

	hash, err := testStore.BlockRepo().GetHash(ctx, chainID, 10)
	if err != nil {
		t.Fatalf("get hash: %v", err)
	}
	if hash != "0xblockhash10" {
		t.Errorf("expected 0xblockhash10, got %q", hash)
	}
}

// TestBlockRepo_DeleteFrom verifies that DeleteFrom removes blocks at or above
// the specified block number while leaving earlier blocks intact.
//
// This is used during reorg recovery: when the indexer detects that block N
// has a different hash than what it stored, it deletes blocks from N onward
// and re-processes them.
func TestBlockRepo_DeleteFrom(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	chainID := int64(324)

	// Insert 3 consecutive blocks: 10, 11, 12
	for _, bn := range []uint64{10, 11, 12} {
		hash := fmt.Sprintf("0xhash%d", bn)
		parent := fmt.Sprintf("0xparent%d", bn)
		if err := testStore.BlockRepo().Insert(ctx, chainID, bn, hash, parent); err != nil {
			t.Fatalf("insert block %d: %v", bn, err)
		}
	}

	// Delete from block 11 onward (blocks 11 and 12 should be removed)
	if err := testStore.BlockRepo().DeleteFrom(ctx, chainID, 11); err != nil {
		t.Fatalf("delete from: %v", err)
	}

	// Block 10 should still exist (it's below the delete threshold).
	hash, err := testStore.BlockRepo().GetHash(ctx, chainID, 10)
	if err != nil {
		t.Fatalf("get hash for block 10: %v", err)
	}
	if hash != "0xhash10" {
		t.Errorf("expected 0xhash10, got %q", hash)
	}

	// Blocks 11 and 12 should be gone (GetHash returns "" for missing blocks).
	for _, bn := range []uint64{11, 12} {
		h, err := testStore.BlockRepo().GetHash(ctx, chainID, bn)
		if err != nil {
			t.Fatalf("get hash for block %d: %v", bn, err)
		}
		if h != "" {
			t.Errorf("expected empty hash for deleted block %d, got %q", bn, h)
		}
	}
}

// ===========================================================================
// RawEventRepo tests
// ===========================================================================

// TestRawEventRepo_Insert verifies that a raw event can be inserted and
// its key fields are correctly stored in the database.
//
// Verification uses raw SQL (via testPool) rather than the store interface
// to ensure we're testing what's actually persisted, not just that the
// read path mirrors the write path.
func TestRawEventRepo_Insert(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond) // Postgres has microsecond precision

	event := &cca.RawEvent{
		ChainID:     324,
		BlockNumber: 5,
		BlockHash:   common.HexToHash("0xblockhash5"),
		TxHash:      common.HexToHash("0xtxhash5"),
		LogIndex:    0,
		Address:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		EventName:   "AuctionCreated",
		TopicsJSON:  `["0xtopic0","0xtopic1"]`,
		DataHex:     "0xdeadbeef",
		DecodedJSON: `{"key":"value"}`,
		IndexedAt:   now,
	}

	if err := testStore.RawEventRepo().Insert(ctx, event); err != nil {
		t.Fatalf("insert raw event: %v", err)
	}

	// Verify key fields with a raw SQL query against the verification pool.
	var (
		chainID     int64
		blockNumber uint64
		blockHash   string
		txHash      string
		logIndex    int
		address     string
		eventName   string
	)
	err := testPool.QueryRow(ctx,
		"SELECT chain_id, block_number, block_hash, tx_hash, log_index, address, event_name FROM raw_events WHERE chain_id = $1 AND block_number = $2 AND log_index = $3",
		int64(324), uint64(5), 0,
	).Scan(&chainID, &blockNumber, &blockHash, &txHash, &logIndex, &address, &eventName)
	if err != nil {
		t.Fatalf("query raw event: %v", err)
	}
	if chainID != 324 {
		t.Errorf("chain_id: expected 324, got %d", chainID)
	}
	if blockNumber != 5 {
		t.Errorf("block_number: expected 5, got %d", blockNumber)
	}
	if eventName != "AuctionCreated" {
		t.Errorf("event_name: expected AuctionCreated, got %q", eventName)
	}
}

// TestRawEventRepo_DeleteFromBlock verifies that DeleteFromBlock removes events
// at or above the specified block while keeping earlier events.
// Same pattern as TestBlockRepo_DeleteFrom — validates reorg recovery behavior.
func TestRawEventRepo_DeleteFromBlock(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Insert events at blocks 5 and 6
	for _, bn := range []uint64{5, 6} {
		event := &cca.RawEvent{
			ChainID:     324,
			BlockNumber: bn,
			BlockHash:   common.HexToHash(fmt.Sprintf("0xblockhash%d", bn)),
			TxHash:      common.HexToHash(fmt.Sprintf("0xtx%d", bn)),
			LogIndex:    0,
			Address:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
			EventName:   "AuctionCreated",
			TopicsJSON:  "[]",
			DataHex:     "0x",
			DecodedJSON: "{}",
			IndexedAt:   now,
		}
		if err := testStore.RawEventRepo().Insert(ctx, event); err != nil {
			t.Fatalf("insert raw event at block %d: %v", bn, err)
		}
	}

	// Delete from block 6 onward
	if err := testStore.RawEventRepo().DeleteFromBlock(ctx, 324, 6); err != nil {
		t.Fatalf("delete from block: %v", err)
	}

	// Block 5 event should remain
	var count int
	err := testPool.QueryRow(ctx, "SELECT count(*) FROM raw_events WHERE chain_id = $1 AND block_number = $2", int64(324), uint64(5)).Scan(&count)
	if err != nil {
		t.Fatalf("count block 5: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event at block 5, got %d", count)
	}

	// Block 6 event should be gone
	err = testPool.QueryRow(ctx, "SELECT count(*) FROM raw_events WHERE chain_id = $1 AND block_number = $2", int64(324), uint64(6)).Scan(&count)
	if err != nil {
		t.Fatalf("count block 6: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 events at block 6, got %d", count)
	}
}

// ===========================================================================
// AuctionRepo tests
// ===========================================================================

// newTestAuction builds a fully-populated Auction for testing. The blockNumber
// parameter varies per test so multiple auctions can coexist without primary
// key conflicts (the PK is chain_id + block_number + log_index).
func newTestAuction(blockNumber uint64) *cca.Auction {
	return &cca.Auction{
		AuctionAddress:        common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		Token:                 common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
		TotalSupply:           big.NewInt(1_000_000),
		Currency:              common.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
		TokensRecipient:       common.HexToAddress("0xDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"),
		FundsRecipient:        common.HexToAddress("0xEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"),
		StartBlock:            100,
		EndBlock:              200,
		ClaimBlock:            250,
		TickSpacingQ96:        new(big.Int).Lsh(big.NewInt(1), 96), // 1 << 96 (Q96 format)
		ValidationHook:        common.HexToAddress("0x0000000000000000000000000000000000000000"),
		FloorPriceQ96:         func() *big.Int { v, _ := new(big.Int).SetString("7922816251426433759354395034", 10); return v }(),
		RequiredCurrencyRaised: big.NewInt(500_000),
		AuctionStepsData:      []byte{0x01, 0x02, 0x03},
		EmitterContract:       common.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
		ChainID:               324,
		BlockNumber:           blockNumber,
		TxHash:                common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		LogIndex:              0,
		CreatedAt:             time.Now().UTC().Truncate(time.Microsecond),
	}
}

// TestAuctionRepo_Insert verifies that an auction can be inserted and its key
// fields are correctly stored.
//
// Verification uses raw SQL to check a subset of fields — we don't verify
// every column, just enough to confirm the INSERT mapped all 20 parameters
// to the correct columns (addresses as hex strings, big.Int as NUMERIC, etc.).
func TestAuctionRepo_Insert(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	auction := newTestAuction(5)

	if err := testStore.AuctionRepo().Insert(ctx, auction); err != nil {
		t.Fatalf("insert auction: %v", err)
	}

	// Verify a representative subset of fields via raw SQL.
	var (
		auctionAddr string
		token       string
		totalSupply string
		chainID     int64
		blockNumber uint64
		startBlock  uint64
		endBlock    uint64
	)
	err := testPool.QueryRow(ctx,
		"SELECT auction_address, token, total_supply, chain_id, block_number, start_block, end_block FROM auctions WHERE chain_id = $1 AND block_number = $2 AND log_index = $3",
		int64(324), uint64(5), 0,
	).Scan(&auctionAddr, &token, &totalSupply, &chainID, &blockNumber, &startBlock, &endBlock)
	if err != nil {
		t.Fatalf("query auction: %v", err)
	}

	wantAddr := auction.AuctionAddress.Hex()
	if diff := cmp.Diff(wantAddr, auctionAddr); diff != "" {
		t.Errorf("auction_address mismatch (-want +got):\n%s", diff)
	}
	if chainID != 324 {
		t.Errorf("chain_id: expected 324, got %d", chainID)
	}
	if blockNumber != 5 {
		t.Errorf("block_number: expected 5, got %d", blockNumber)
	}

	// big.Int is stored as NUMERIC text — verify it round-trips correctly.
	wantSupply := auction.TotalSupply.String()
	if totalSupply != wantSupply {
		t.Errorf("total_supply: expected %s, got %s", wantSupply, totalSupply)
	}
	if startBlock != 100 {
		t.Errorf("start_block: expected 100, got %d", startBlock)
	}
	if endBlock != 200 {
		t.Errorf("end_block: expected 200, got %d", endBlock)
	}
}

// TestAuctionRepo_DeleteFromBlock verifies that DeleteFromBlock removes auctions
// at or above the specified block while keeping earlier ones.
func TestAuctionRepo_DeleteFromBlock(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()

	// Insert auctions at blocks 5 and 6.
	// We set LogIndex = bn to avoid primary key conflicts (PK includes log_index).
	for _, bn := range []uint64{5, 6} {
		a := newTestAuction(bn)
		a.LogIndex = uint(bn)
		if err := testStore.AuctionRepo().Insert(ctx, a); err != nil {
			t.Fatalf("insert auction at block %d: %v", bn, err)
		}
	}

	// Delete from block 6 onward
	if err := testStore.AuctionRepo().DeleteFromBlock(ctx, 324, 6); err != nil {
		t.Fatalf("delete from block: %v", err)
	}

	// Block 5 auction should remain
	var count int
	err := testPool.QueryRow(ctx, "SELECT count(*) FROM auctions WHERE chain_id = $1 AND block_number = $2", int64(324), uint64(5)).Scan(&count)
	if err != nil {
		t.Fatalf("count block 5: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 auction at block 5, got %d", count)
	}

	// Block 6 auction should be gone
	err = testPool.QueryRow(ctx, "SELECT count(*) FROM auctions WHERE chain_id = $1 AND block_number = $2", int64(324), uint64(6)).Scan(&count)
	if err != nil {
		t.Fatalf("count block 6: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 auctions at block 6, got %d", count)
	}
}

// ===========================================================================
// WithTx tests
// ===========================================================================

// TestWithTx_CommitOnSuccess verifies that when the callback returns nil,
// the transaction is committed and data persists.
func TestWithTx_CommitOnSuccess(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()

	err := testStore.WithTx(ctx, func(txStore store.Store) error {
		return txStore.CursorRepo().Upsert(ctx, 324, 42, "0xcommitted")
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Data should be visible after WithTx returns (tx was committed).
	blockNum, blockHash, err := testStore.CursorRepo().Get(ctx, 324)
	if err != nil {
		t.Fatalf("get after commit: %v", err)
	}
	if blockNum != 42 {
		t.Errorf("expected blockNumber 42, got %d", blockNum)
	}
	if blockHash != "0xcommitted" {
		t.Errorf("expected blockHash 0xcommitted, got %q", blockHash)
	}
}

// TestWithTx_RollbackOnError verifies that when the callback returns an error,
// the transaction is rolled back and no data persists.
//
// This is critical for the indexer's atomicity guarantee: if any write fails
// during a tick, the entire tick's changes are discarded and the cursor is
// not advanced, so the tick can be retried safely.
func TestWithTx_RollbackOnError(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()

	rollbackErr := fmt.Errorf("intentional failure")
	err := testStore.WithTx(ctx, func(txStore store.Store) error {
		// This write happens inside the transaction...
		if uErr := txStore.CursorRepo().Upsert(ctx, 324, 99, "0xrolledback"); uErr != nil {
			t.Fatalf("upsert in tx: %v", uErr)
		}
		// ...but the callback returns an error, triggering rollback.
		return rollbackErr
	})
	if err == nil {
		t.Fatal("expected error from WithTx, got nil")
	}

	// Data should NOT persist — the transaction was rolled back.
	blockNum, blockHash, err := testStore.CursorRepo().Get(ctx, 324)
	if err != nil {
		t.Fatalf("get after rollback: %v", err)
	}
	if blockNum != 0 {
		t.Errorf("expected blockNumber 0 after rollback, got %d", blockNum)
	}
	if blockHash != "" {
		t.Errorf("expected empty blockHash after rollback, got %q", blockHash)
	}
}
