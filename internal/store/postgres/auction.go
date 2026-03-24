package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// AuctionRepo implements store.AuctionRepository backed by the "auctions" table.
type AuctionRepo struct {
	db querier
}

// Insert stores a decoded auction record. Addresses and big.Int values are
// converted to their string/hex representations for storage in TEXT/NUMERIC
// columns, since PostgreSQL has no native Ethereum address type.
func (r *AuctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO auctions (auction_address, token, total_supply, currency, tokens_recipient, funds_recipient, start_block, end_block, claim_block, tick_spacing_q96, validation_hook, floor_price_q96, required_currency_raised, auction_steps_data, emitter_contract, chain_id, block_number, tx_hash, log_index, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)",
		auction.AuctionAddress.Hex(),
		auction.Token.Hex(),
		auction.TotalSupply.String(),
		auction.Currency.Hex(),
		auction.TokensRecipient.Hex(),
		auction.FundsRecipient.Hex(),
		auction.StartBlock,
		auction.EndBlock,
		auction.ClaimBlock,
		auction.TickSpacingQ96.String(),
		auction.ValidationHook.Hex(),
		auction.FloorPriceQ96.String(),
		auction.RequiredCurrencyRaised.String(),
		auction.AuctionStepsData,
		auction.EmitterContract.Hex(),
		auction.ChainID,
		auction.BlockNumber,
		auction.TxHash.Hex(),
		auction.LogIndex,
		auction.CreatedAt,
	)
	return err
}

// DeleteFromBlock removes all auctions for the given chain at or above fromBlock.
func (r *AuctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.db.Exec(ctx,
		"DELETE FROM auctions WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}
