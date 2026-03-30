package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

type auctionRepo struct{ q querier }

func lowerHex(addr common.Address) string {
	return strings.ToLower(addr.Hex())
}

func parseBigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return big.NewInt(0)
	}
	return n
}

func (r *auctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO event_ccaf_auction_created (
			chain_id, auction_address, token, amount, currency,
			tokens_recipient, funds_recipient, start_block, end_block, claim_block,
			tick_spacing, validation_hook, floor_price, required_currency_raised,
			block_number, tx_hash, log_index
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT DO NOTHING`,
		auction.ChainID,
		lowerHex(auction.AuctionAddress),
		lowerHex(auction.Token),
		auction.Amount.String(),
		lowerHex(auction.Currency),
		lowerHex(auction.TokensRecipient),
		lowerHex(auction.FundsRecipient),
		auction.StartBlock,
		auction.EndBlock,
		auction.ClaimBlock,
		auction.TickSpacing.String(),
		lowerHex(auction.ValidationHook),
		auction.FloorPrice.String(),
		auction.RequiredCurrencyRaised.String(),
		auction.BlockNumber,
		auction.TxHash.Hex(),
		auction.LogIndex,
	)
	return err
}

func (r *auctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		"DELETE FROM event_ccaf_auction_created WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}

func (r *auctionRepo) GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
	var (
		auctionAddr, token, currency, tokensRecipient, fundsRecipient string
		validationHook, txHash                                        string
		amountStr, tickSpacingStr, floorPriceStr, reqCurrRaisedStr    string
		startBlock, endBlock, claimBlock, blockNumber                 uint64
		logIndex                                                      uint
		chainIDVal                                                    int64
	)

	err := r.q.QueryRow(ctx,
		`SELECT chain_id, auction_address, token, amount, currency,
			tokens_recipient, funds_recipient, start_block, end_block, claim_block,
			tick_spacing, validation_hook, floor_price, required_currency_raised,
			block_number, tx_hash, log_index
		 FROM event_ccaf_auction_created
		 WHERE chain_id = $1 AND auction_address = $2`,
		chainID, auctionAddress,
	).Scan(
		&chainIDVal, &auctionAddr, &token, &amountStr, &currency,
		&tokensRecipient, &fundsRecipient, &startBlock, &endBlock, &claimBlock,
		&tickSpacingStr, &validationHook, &floorPriceStr, &reqCurrRaisedStr,
		&blockNumber, &txHash, &logIndex,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	amount := parseBigInt(amountStr)
	tickSpacing := parseBigInt(tickSpacingStr)
	floorPrice := parseBigInt(floorPriceStr)
	reqCurrRaised := parseBigInt(reqCurrRaisedStr)

	return &cca.Auction{
		ChainID:                chainIDVal,
		AuctionAddress:         common.HexToAddress(auctionAddr),
		Token:                  common.HexToAddress(token),
		Amount:                 amount,
		Currency:               common.HexToAddress(currency),
		TokensRecipient:        common.HexToAddress(tokensRecipient),
		FundsRecipient:         common.HexToAddress(fundsRecipient),
		StartBlock:             startBlock,
		EndBlock:               endBlock,
		ClaimBlock:             claimBlock,
		TickSpacing:            tickSpacing,
		ValidationHook:         common.HexToAddress(validationHook),
		FloorPrice:             floorPrice,
		RequiredCurrencyRaised: reqCurrRaised,
		BlockNumber:            blockNumber,
		TxHash:                 common.HexToHash(txHash),
		LogIndex:               logIndex,
	}, nil
}

// List returns auctions ordered by (block_number, log_index) descending with cursor-based pagination.
func (r *auctionRepo) List(ctx context.Context, chainID int64, params store.PaginationParams) ([]*cca.Auction, error) {
	return nil, fmt.Errorf("auctionRepo.List: not yet implemented")
}
