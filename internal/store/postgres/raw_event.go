package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type rawEventRepo struct{ q querier }

func (r *rawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO raw_events (chain_id, block_number, block_hash, tx_hash, log_index, address, event_name, topics, data, decoded)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT DO NOTHING`,
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
	)
	return err
}

func (r *rawEventRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		"DELETE FROM raw_events WHERE chain_id = $1 AND block_number >= $2",
		chainID, fromBlock,
	)
	return err
}
