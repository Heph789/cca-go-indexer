package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// RawEventRepo implements store.RawEventRepository.
type RawEventRepo struct {
	db querier
}

// Insert stores a raw event record.
func (r *RawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO raw_events (chain_id, block_number, block_hash, tx_hash, log_index, address, event_name, topics_json, data_hex, decoded_json, indexed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)",
		event.ChainID,
		event.BlockNumber,
		event.BlockHash.Hex(),
		event.TxHash.Hex(),
		event.LogIndex,
		event.Address.Hex(),
		event.EventName,
		event.TopicsJSON,
		event.DataHex,
		event.DecodedJSON,
		event.IndexedAt,
	)
	return err
}

// DeleteFromBlock removes all raw events for the given chain at or above fromBlock.
func (r *RawEventRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.db.Exec(ctx,
		"DELETE FROM raw_events WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}
