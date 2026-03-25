package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// AuctionRepo implements store.AuctionRepository.
type AuctionRepo struct {
	db querier
}

// Insert stores an auction record.
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
