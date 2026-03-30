package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

type bidRepo struct{ q querier }

// Insert stores a bid record in the database. If a record with the same
// primary key already exists, the insert is silently skipped (ON CONFLICT DO NOTHING).
func (r *bidRepo) Insert(ctx context.Context, bid *cca.Bid) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO event_cca_bid_submitted (
			chain_id, auction_address, id, owner, price_q96, amount,
			block_number, block_time, tx_hash, log_index
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT DO NOTHING`,
		bid.ChainID,
		lowerHex(bid.AuctionAddress),
		bid.ID,
		lowerHex(bid.Owner),
		bid.PriceQ96.String(),
		bid.Amount.String(),
		bid.BlockNumber,
		bid.BlockTime,
		bid.TxHash.Hex(),
		bid.LogIndex,
	)
	return err
}

// DeleteFromBlock removes bids at or after the given block number.
func (r *bidRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		"DELETE FROM event_cca_bid_submitted WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}

// ListByAuction returns bids for an auction in descending order with cursor-based pagination.
func (r *bidRepo) ListByAuction(ctx context.Context, chainID int64, auctionAddress string, params store.PaginationParams) ([]*cca.Bid, error) {
	return r.listBids(ctx, chainID, auctionAddress, "", params)
}

// ListByAuctionAndOwner returns bids for an auction filtered by owner address.
func (r *bidRepo) ListByAuctionAndOwner(ctx context.Context, chainID int64, auctionAddress string, owner string, params store.PaginationParams) ([]*cca.Bid, error) {
	return r.listBids(ctx, chainID, auctionAddress, owner, params)
}

// listBids is a shared helper that builds a dynamic query with optional owner
// filter and cursor-based pagination.
func (r *bidRepo) listBids(ctx context.Context, chainID int64, auctionAddress string, owner string, params store.PaginationParams) ([]*cca.Bid, error) {
	query := `SELECT chain_id, auction_address, id, owner, price_q96, amount,
		block_number, block_time, tx_hash, log_index
	 FROM event_cca_bid_submitted
	 WHERE chain_id = $1 AND auction_address = $2`

	args := []any{chainID, auctionAddress}
	argIdx := 3

	if owner != "" {
		query += fmt.Sprintf(" AND owner = $%d", argIdx)
		args = append(args, owner)
		argIdx++
	}

	if params.CursorBlockNumber != nil && params.CursorLogIndex != nil {
		query += fmt.Sprintf(" AND (block_number, log_index) < ($%d, $%d)", argIdx, argIdx+1)
		args = append(args, *params.CursorBlockNumber, *params.CursorLogIndex)
		argIdx += 2
	}

	query += " ORDER BY block_number DESC, log_index DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, params.Limit)

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*cca.Bid
	for rows.Next() {
		bid, err := scanBid(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, bid)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetPrevTickPrice returns the largest distinct bid price strictly below maxPrice.
// Returns an empty string when no qualifying price exists.
func (r *bidRepo) GetPrevTickPrice(ctx context.Context, chainID int64, auctionAddress string, maxPrice string) (string, error) {
	var price string
	err := r.q.QueryRow(ctx,
		`SELECT DISTINCT price_q96 FROM event_cca_bid_submitted
		 WHERE chain_id = $1 AND auction_address = $2 AND price_q96 < $3
		 ORDER BY price_q96 DESC LIMIT 1`,
		chainID, auctionAddress, maxPrice,
	).Scan(&price)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return price, nil
}

// scanBid scans a single row from the event_cca_bid_submitted table into a Bid struct.
func scanBid(row pgx.Rows) (*cca.Bid, error) {
	var (
		chainIDVal                    int64
		auctionAddr, ownerStr, txHash string
		priceQ96Str, amountStr        string
		blockNumber                   uint64
		logIndex                      uint
		b                             cca.Bid
	)

	if err := row.Scan(
		&chainIDVal, &auctionAddr, &b.ID, &ownerStr, &priceQ96Str, &amountStr,
		&blockNumber, &b.BlockTime, &txHash, &logIndex,
	); err != nil {
		return nil, err
	}

	b.ChainID = chainIDVal
	b.AuctionAddress = common.HexToAddress(auctionAddr)
	b.Owner = common.HexToAddress(ownerStr)
	b.PriceQ96 = parseBigInt(priceQ96Str)
	b.Amount = parseBigInt(amountStr)
	b.BlockNumber = blockNumber
	b.TxHash = common.HexToHash(txHash)
	b.LogIndex = logIndex

	return &b, nil
}
