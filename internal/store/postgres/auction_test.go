package postgres_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAuctionRepo_Insert(t *testing.T) {
	truncateAll(t)

	ctx := context.Background()
	auction := newTestAuction(5)

	if err := testStore.AuctionRepo().Insert(ctx, auction); err != nil {
		t.Fatalf("insert auction: %v", err)
	}

	var got struct {
		AuctionAddr string
		Token       string
		TotalSupply string
		ChainID     int64
		BlockNumber uint64
		StartBlock  uint64
		EndBlock    uint64
	}
	err := testPool.QueryRow(ctx,
		"SELECT auction_address, token, total_supply, chain_id, block_number, start_block, end_block FROM auctions WHERE chain_id = $1 AND block_number = $2 AND log_index = $3",
		int64(324), uint64(5), 0,
	).Scan(&got.AuctionAddr, &got.Token, &got.TotalSupply, &got.ChainID, &got.BlockNumber, &got.StartBlock, &got.EndBlock)
	if err != nil {
		t.Fatalf("query auction: %v", err)
	}

	want := struct {
		AuctionAddr string
		Token       string
		TotalSupply string
		ChainID     int64
		BlockNumber uint64
		StartBlock  uint64
		EndBlock    uint64
	}{
		AuctionAddr: auction.AuctionAddress.Hex(),
		Token:       auction.Token.Hex(),
		TotalSupply: auction.TotalSupply.String(),
		ChainID:     324,
		BlockNumber: 5,
		StartBlock:  100,
		EndBlock:    200,
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("auction mismatch (-want +got):\n%s", diff)
	}
}
