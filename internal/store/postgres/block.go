package postgres

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

type blockRepo struct{ q querier }

func (r *blockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash common.Hash) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO indexed_blocks (chain_id, block_number, block_hash, parent_hash)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`,
		chainID, blockNumber, blockHash.Hex(), parentHash.Hex(),
	)
	return err
}

func (r *blockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (common.Hash, error) {
	var hash string

	err := r.q.QueryRow(ctx,
		"SELECT block_hash FROM indexed_blocks WHERE chain_id = $1 AND block_number = $2",
		chainID, blockNumber,
	).Scan(&hash)

	if errors.Is(err, pgx.ErrNoRows) {
		return common.Hash{}, nil
	}
	if err != nil {
		return common.Hash{}, err
	}

	return common.HexToHash(hash), nil
}

func (r *blockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		"DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}
