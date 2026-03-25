package postgres_test

import (
	"context"
	"testing"
)

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

func TestCursorRepo_UpsertAndGet(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	chainID := int64(324)

	tests := []struct {
		name      string
		block     uint64
		hash      string
	}{
		{"initial insert", 100, "0xaaa"},
		{"upsert overwrites", 200, "0xbbb"},
	}

	for _, tt := range tests {
		if err := testStore.CursorRepo().Upsert(ctx, chainID, tt.block, tt.hash); err != nil {
			t.Fatalf("%s: upsert: %v", tt.name, err)
		}
		blockNum, blockHash, err := testStore.CursorRepo().Get(ctx, chainID)
		if err != nil {
			t.Fatalf("%s: get: %v", tt.name, err)
		}
		if blockNum != tt.block {
			t.Errorf("%s: blockNumber = %d, want %d", tt.name, blockNum, tt.block)
		}
		if blockHash != tt.hash {
			t.Errorf("%s: blockHash = %q, want %q", tt.name, blockHash, tt.hash)
		}
	}
}
