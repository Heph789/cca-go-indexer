package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/cca/go-indexer/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) store.Store {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	s, err := New(ctx, dbURL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew_ReturnsWorkingStore(t *testing.T) {
	testStore(t)
}

func TestNew_ErrorForInvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, "postgres://invalid:5432/nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestRunMigrations_TablesExist(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)

	expectedTables := []string{
		"indexer_cursors",
		"raw_events",
		"event_ccaf_auction_created",
		"indexed_blocks",
		"watched_contracts",
	}

	ctx := context.Background()
	for _, table := range expectedTables {
		var exists bool
		err := ps.pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("checking table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %s to exist", table)
		}
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	// First run happens inside testStore; run again explicitly.
	_ = testStore(t)

	err := runMigrations(dbURL)
	if err != nil {
		t.Fatalf("second runMigrations call failed: %v", err)
	}
}

func TestQuerier_ReturnsPoolOutsideTx(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)

	got := ps.q()
	if got != ps.pool {
		t.Error("expected querier to return pool outside tx")
	}
}

func TestQuerier_ReturnsTxInsideWithTx(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	err := s.WithTx(ctx, func(txStore store.Store) error {
		txPs := txStore.(*pgStore)
		if txPs.tx == nil {
			t.Error("expected tx to be non-nil inside WithTx")
		}
		if txPs.q() == txPs.pool {
			t.Error("expected querier to return tx, not pool, inside WithTx")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)
	ctx := context.Background()

	// Clean up test data first.
	_, _ = ps.pool.Exec(ctx, "DELETE FROM indexer_cursors WHERE chain_id = 999")

	err := s.WithTx(ctx, func(txStore store.Store) error {
		txPs := txStore.(*pgStore)
		_, err := txPs.q().Exec(ctx,
			"INSERT INTO indexer_cursors (chain_id, last_block, last_block_hash) VALUES ($1, $2, $3)",
			999, 100, "0xhash",
		)
		if err != nil {
			return err
		}
		return fmt.Errorf("intentional rollback")
	})
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	// Row should NOT exist after rollback.
	var count int
	err = ps.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM indexer_cursors WHERE chain_id = 999",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query after rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after rollback, got %d", count)
	}
}

func TestWithTx_CommitsOnNilReturn(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)
	ctx := context.Background()

	// Clean up test data first.
	_, _ = ps.pool.Exec(ctx, "DELETE FROM indexer_cursors WHERE chain_id = 998")

	err := s.WithTx(ctx, func(txStore store.Store) error {
		txPs := txStore.(*pgStore)
		_, err := txPs.q().Exec(ctx,
			"INSERT INTO indexer_cursors (chain_id, last_block, last_block_hash) VALUES ($1, $2, $3)",
			998, 200, "0xcommitted",
		)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Row should exist after commit.
	var lastBlock int64
	err = ps.pool.QueryRow(ctx,
		"SELECT last_block FROM indexer_cursors WHERE chain_id = 998",
	).Scan(&lastBlock)
	if err != nil {
		t.Fatalf("query after commit: %v", err)
	}
	if lastBlock != 200 {
		t.Errorf("expected last_block 200, got %d", lastBlock)
	}

	// Clean up.
	_, _ = ps.pool.Exec(ctx, "DELETE FROM indexer_cursors WHERE chain_id = 998")
}

// TestNew_SetsIdleInTransactionSessionTimeout verifies that postgres.New
// configures an AfterConnect callback on the pgxpool that sets
// idle_in_transaction_session_timeout = '30s' on every connection. This
// prevents long-running idle transactions from holding locks indefinitely.
//
// The test acquires a connection from the pool and uses SHOW to read back
// the session-level setting, comparing it to the expected value.
func TestNew_SetsIdleInTransactionSessionTimeout(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)
	ctx := context.Background()

	// wantTimeout is the value we expect idle_in_transaction_session_timeout
	// to be set to by the AfterConnect callback. Postgres reports this as "30s".
	wantTimeout := "30s"

	conn, err := ps.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire connection: %v", err)
	}
	defer conn.Release()

	// Query the session-level setting to confirm AfterConnect applied it.
	var got string
	err = conn.QueryRow(ctx, "SHOW idle_in_transaction_session_timeout").Scan(&got)
	if err != nil {
		t.Fatalf("SHOW idle_in_transaction_session_timeout: %v", err)
	}

	if got != wantTimeout {
		t.Errorf("idle_in_transaction_session_timeout = %q, want %q", got, wantTimeout)
	}
}

// TestNew_IdleInTransactionTimeout_ConsistentAcrossConnections verifies that
// every connection obtained from the pool has the timeout set, not just the
// first one. This catches bugs where the AfterConnect callback is missing and
// only an initial Exec was used.
func TestNew_IdleInTransactionTimeout_ConsistentAcrossConnections(t *testing.T) {
	s := testStore(t)
	ps := s.(*pgStore)
	ctx := context.Background()

	// wantTimeout is the expected value for every pooled connection.
	wantTimeout := "30s"

	// numConns is the number of distinct connections to check. We acquire
	// multiple connections simultaneously to force the pool to create new ones.
	numConns := 3

	conns := make([]*pgxpool.Conn, 0, numConns)
	defer func() {
		for _, c := range conns {
			c.Release()
		}
	}()

	// Acquire several connections at once so the pool must open new ones.
	for i := range numConns {
		c, err := ps.pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire connection %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	// Verify each connection has the timeout set.
	for i, c := range conns {
		var got string
		err := c.QueryRow(ctx, "SHOW idle_in_transaction_session_timeout").Scan(&got)
		if err != nil {
			t.Fatalf("SHOW on connection %d: %v", i, err)
		}
		if got != wantTimeout {
			t.Errorf("connection %d: idle_in_transaction_session_timeout = %q, want %q", i, got, wantTimeout)
		}
	}
}

func TestClose_ReleasesPool(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	s, err := New(ctx, dbURL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.Close()

	// After close, the pool should be unusable.
	ps := s.(*pgStore)
	err = ps.pool.Ping(ctx)
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}
