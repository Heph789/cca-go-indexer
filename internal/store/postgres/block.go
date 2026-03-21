package postgres

import "context"

type blockRepo struct {
	q querier
}

// Insert records a block's hash and parent hash for reorg detection.
// Each indexed block is stored so the indexer can verify chain continuity
// before processing new blocks.
func (r *blockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	// TODO: INSERT INTO indexed_blocks (chain_id, block_number, block_hash, parent_hash)
	//       VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	panic("not implemented")
}

// GetHash returns the stored hash for a specific block.
// Used in reorg detection: the indexer compares this against the
// chain's current hash for the same block number.
func (r *blockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error) {
	// TODO: SELECT block_hash FROM indexed_blocks WHERE chain_id = $1 AND block_number = $2
	panic("not implemented")
}

// DeleteFrom removes all block records at or after fromBlock.
// Called during reorg rollback to discard orphaned block hashes.
func (r *blockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	// TODO: DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number >= $2
	panic("not implemented")
}
