package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type cursorRepo struct{ q querier }

func (r *cursorRepo) Get(ctx context.Context, chainID int64) (uint64, string, error) {
	var blockNumber uint64
	var blockHash string

	err := r.q.QueryRow(ctx,
		"SELECT last_block, last_block_hash FROM indexer_cursors WHERE chain_id = $1",
		chainID,
	).Scan(&blockNumber, &blockHash)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}

	return blockNumber, blockHash, nil
}

func (r *cursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO indexer_cursors (chain_id, last_block, last_block_hash)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (chain_id) DO UPDATE
		 SET last_block = EXCLUDED.last_block,
		     last_block_hash = EXCLUDED.last_block_hash,
		     updated_at = NOW()`,
		chainID, blockNumber, blockHash,
	)
	return err
}
