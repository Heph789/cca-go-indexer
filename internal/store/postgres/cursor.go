package postgres

import "context"

type cursorRepo struct {
	q querier
}

// Get returns the current cursor position for a chain.
// The cursor is the last fully-processed block number and its hash.
// Returns (0, "", nil) if no cursor exists yet (fresh start).
func (r *cursorRepo) Get(ctx context.Context, chainID int64) (uint64, string, error) {
	// TODO: SELECT last_block, last_block_hash FROM indexer_cursors WHERE chain_id = $1
	// If no rows, return (0, "", nil) — indexer will start from StartBlock config.
	panic("not implemented")
}

// Upsert creates or updates the cursor for a chain.
// Called at the end of each batch inside the WithTx transaction,
// ensuring the cursor never advances past committed data.
func (r *cursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	// TODO: INSERT INTO indexer_cursors (chain_id, last_block, last_block_hash, created_at, updated_at)
	//       VALUES ($1, $2, $3, NOW(), NOW())
	//       ON CONFLICT (chain_id) DO UPDATE SET last_block = $2, last_block_hash = $3, updated_at = NOW()
	panic("not implemented")
}
