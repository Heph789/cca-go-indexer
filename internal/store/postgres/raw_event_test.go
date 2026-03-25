package postgres_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRawEventRepo_Insert(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	event := newTestRawEvent(5)

	if err := testStore.RawEventRepo().Insert(ctx, event); err != nil {
		t.Fatalf("insert raw event: %v", err)
	}

	var got struct {
		ChainID     int64
		BlockNumber uint64
		BlockHash   string
		TxHash      string
		LogIndex    int
		Address     string
		EventName   string
	}
	err := testPool.QueryRow(ctx,
		"SELECT chain_id, block_number, block_hash, tx_hash, log_index, address, event_name FROM raw_events WHERE chain_id = $1 AND block_number = $2 AND log_index = $3",
		int64(324), uint64(5), 0,
	).Scan(&got.ChainID, &got.BlockNumber, &got.BlockHash, &got.TxHash, &got.LogIndex, &got.Address, &got.EventName)
	if err != nil {
		t.Fatalf("query raw event: %v", err)
	}

	want := struct {
		ChainID     int64
		BlockNumber uint64
		BlockHash   string
		TxHash      string
		LogIndex    int
		Address     string
		EventName   string
	}{
		ChainID:     324,
		BlockNumber: 5,
		BlockHash:   event.BlockHash.Hex(),
		TxHash:      event.TxHash.Hex(),
		LogIndex:    0,
		Address:     event.Address.Hex(),
		EventName:   "AuctionCreated",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("raw event mismatch (-want +got):\n%s", diff)
	}
}
