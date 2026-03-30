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

type checkpointRepo struct{ q querier }

// Insert stores a checkpoint record in the database. If a record with the same
// primary key already exists, the insert is silently skipped (ON CONFLICT DO NOTHING).
func (r *checkpointRepo) Insert(ctx context.Context, checkpoint *cca.Checkpoint) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO event_cca_checkpoint_updated (
			chain_id, auction_address, block_number, clearing_price_q96, cumulative_mps,
			tx_block_number, tx_block_time, tx_hash, log_index
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT DO NOTHING`,
		checkpoint.ChainID,
		lowerHex(checkpoint.AuctionAddress),
		checkpoint.BlockNumber,
		checkpoint.ClearingPriceQ96.String(),
		checkpoint.CumulativeMps,
		checkpoint.TxBlockNumber,
		checkpoint.TxBlockTime,
		checkpoint.TxHash.Hex(),
		checkpoint.LogIndex,
	)
	return err
}

// DeleteFromBlock removes checkpoints whose tx_block_number is at or after fromBlock.
func (r *checkpointRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		"DELETE FROM event_cca_checkpoint_updated WHERE chain_id = $1 AND tx_block_number >= $2",
		chainID, fromBlock,
	)
	return err
}

// GetLatest returns the checkpoint with the highest block_number for a given auction.
// Returns (nil, nil) when no checkpoint exists for the specified auction.
func (r *checkpointRepo) GetLatest(ctx context.Context, chainID int64, auctionAddress string) (*cca.Checkpoint, error) {
	var (
		chainIDVal                    int64
		auctionAddr, clearingPriceStr string
		txHash                        string
		blockNumber, txBlockNumber    uint64
		cumulativeMps                 uint32
		logIndex                      uint
		c                             cca.Checkpoint
	)

	err := r.q.QueryRow(ctx,
		`SELECT chain_id, auction_address, block_number, clearing_price_q96, cumulative_mps,
			tx_block_number, tx_block_time, tx_hash, log_index
		 FROM event_cca_checkpoint_updated
		 WHERE chain_id = $1 AND auction_address = $2
		 ORDER BY block_number DESC LIMIT 1`,
		chainID, auctionAddress,
	).Scan(
		&chainIDVal, &auctionAddr, &blockNumber, &clearingPriceStr, &cumulativeMps,
		&txBlockNumber, &c.TxBlockTime, &txHash, &logIndex,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	c.ChainID = chainIDVal
	c.AuctionAddress = common.HexToAddress(auctionAddr)
	c.BlockNumber = blockNumber
	c.ClearingPriceQ96 = parseBigInt(clearingPriceStr)
	c.CumulativeMps = cumulativeMps
	c.TxBlockNumber = txBlockNumber
	c.TxHash = common.HexToHash(txHash)
	c.LogIndex = logIndex

	return &c, nil
}

// ListByAuction returns checkpoints for an auction in descending order with cursor-based pagination.
func (r *checkpointRepo) ListByAuction(ctx context.Context, chainID int64, auctionAddress string, params store.PaginationParams) ([]*cca.Checkpoint, error) {
	query := `SELECT chain_id, auction_address, block_number, clearing_price_q96, cumulative_mps,
		tx_block_number, tx_block_time, tx_hash, log_index
	 FROM event_cca_checkpoint_updated
	 WHERE chain_id = $1 AND auction_address = $2`

	args := []any{chainID, auctionAddress}
	argIdx := 3

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

	var result []*cca.Checkpoint
	for rows.Next() {
		var (
			chainIDVal                    int64
			auctionAddr, clearingPriceStr string
			txHash                        string
			blockNumber, txBlockNumber    uint64
			cumulativeMps                 uint32
			logIndex                      uint
			c                             cca.Checkpoint
		)

		if err := rows.Scan(
			&chainIDVal, &auctionAddr, &blockNumber, &clearingPriceStr, &cumulativeMps,
			&txBlockNumber, &c.TxBlockTime, &txHash, &logIndex,
		); err != nil {
			return nil, err
		}

		c.ChainID = chainIDVal
		c.AuctionAddress = common.HexToAddress(auctionAddr)
		c.BlockNumber = blockNumber
		c.ClearingPriceQ96 = parseBigInt(clearingPriceStr)
		c.CumulativeMps = cumulativeMps
		c.TxBlockNumber = txBlockNumber
		c.TxHash = common.HexToHash(txHash)
		c.LogIndex = logIndex

		result = append(result, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
