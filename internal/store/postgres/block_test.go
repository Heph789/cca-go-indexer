package postgres_test

import (
	"context"
	"testing"
)

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
