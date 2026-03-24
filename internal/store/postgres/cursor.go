package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CursorRepo implements store.CursorRepository backed by the "cursors" table.
// There is one row per chain, keyed by chain_id. The cursor is upserted at
// the end of each tick so the indexer can resume from the last processed block.
type CursorRepo struct {
	db querier
}

// Get returns the current cursor for the given chain. If no cursor exists
// (first run), it returns (0, "", nil) — the zero values signal the indexer
// to start from the configured StartBlock.
func (r *CursorRepo) Get(ctx context.Context, chainID int64) (uint64, string, error) {
	var blockNumber uint64
	var blockHash string

	err := r.db.QueryRow(ctx,
		"SELECT block_number, block_hash FROM cursors WHERE chain_id = $1",
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

// Upsert inserts or updates the cursor for the given chain. Uses PostgreSQL's
// ON CONFLICT ... DO UPDATE (upsert) so the first run inserts and subsequent
// runs update the same row.
func (r *CursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO cursors (chain_id, block_number, block_hash) VALUES ($1, $2, $3) ON CONFLICT (chain_id) DO UPDATE SET block_number = EXCLUDED.block_number, block_hash = EXCLUDED.block_hash`,
		chainID, blockNumber, blockHash,
	)
	return err
}
