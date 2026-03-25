package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cca/go-indexer/internal/store"
)

func TestWithTx_CommitOnSuccess(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()

	err := testStore.WithTx(ctx, func(txStore store.Store) error {
		return txStore.CursorRepo().Upsert(ctx, 324, 42, "0xcommitted")
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

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

func TestWithTx_RollbackOnError(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()

	rollbackErr := fmt.Errorf("intentional failure")
	err := testStore.WithTx(ctx, func(txStore store.Store) error {
		if uErr := txStore.CursorRepo().Upsert(ctx, 324, 99, "0xrolledback"); uErr != nil {
			t.Fatalf("upsert in tx: %v", uErr)
		}
		return rollbackErr
	})
	if err == nil {
		t.Fatal("expected error from WithTx, got nil")
	}

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
