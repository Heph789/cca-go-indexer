package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CursorRepo implements store.CursorRepository.
type CursorRepo struct {
	db querier
}

// Get returns the current cursor for the given chain. If no cursor exists,
// it returns (0, "", nil).
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

// Upsert inserts or updates the cursor for the given chain.
func (r *CursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO cursors (chain_id, block_number, block_hash) VALUES ($1, $2, $3) ON CONFLICT (chain_id) DO UPDATE SET block_number = EXCLUDED.block_number, block_hash = EXCLUDED.block_hash`,
		chainID, blockNumber, blockHash,
	)
	return err
}
