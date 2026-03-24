package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// BlockRepo implements store.BlockRepository.
type BlockRepo struct {
	db querier
}

// Insert stores a block record.
func (r *BlockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO blocks (chain_id, block_number, block_hash, parent_hash) VALUES ($1, $2, $3, $4)",
		chainID, blockNumber, blockHash, parentHash,
	)
	return err
}

// GetHash returns the block hash for the given chain and block number.
// If no row exists it returns ("", nil).
func (r *BlockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error) {
	var hash string
	err := r.db.QueryRow(ctx,
		"SELECT block_hash FROM blocks WHERE chain_id = $1 AND block_number = $2",
		chainID, blockNumber,
	).Scan(&hash)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
}

// DeleteFrom removes all blocks for the given chain at or above fromBlock.
func (r *BlockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.db.Exec(ctx,
		"DELETE FROM blocks WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}
