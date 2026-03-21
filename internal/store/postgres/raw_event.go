package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type rawEventRepo struct {
	q querier
}

// Insert writes a raw log to the raw_events table.
// Every event is stored here regardless of type, providing an audit
// trail and enabling replay if event schemas change.
// Uses ON CONFLICT DO NOTHING for idempotency.
func (r *rawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
	// TODO: INSERT INTO raw_events (chain_id, block_number, block_hash, tx_hash,
	//       log_index, address, event_name, topics, data, decoded)
	//       VALUES ($1, $2, ...) ON CONFLICT DO NOTHING
	panic("not implemented")
}

// DeleteFromBlock removes all raw events at or after fromBlock for a chain.
func (r *rawEventRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	// TODO: DELETE FROM raw_events WHERE chain_id = $1 AND block_number >= $2
	panic("not implemented")
}
