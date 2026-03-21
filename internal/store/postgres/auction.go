package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type auctionRepo struct {
	q querier
}

// Insert writes a decoded AuctionCreated event to the auctions table.
// Uses ON CONFLICT DO NOTHING for idempotency — re-processing the
// same block range after a restart is safe.
func (r *auctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	// TODO: INSERT INTO auctions (chain_id, auction_address, token_out, currency_in,
	//       owner, start_time, end_time, block_number, tx_hash, log_index)
	//       VALUES ($1, $2, ...) ON CONFLICT DO NOTHING
	panic("not implemented")
}

// DeleteFromBlock removes all auctions at or after fromBlock for a chain.
// Called during reorg rollback to remove data from orphaned blocks.
func (r *auctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	// TODO: DELETE FROM auctions WHERE chain_id = $1 AND block_number >= $2
	panic("not implemented")
}
