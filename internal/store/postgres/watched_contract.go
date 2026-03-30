package postgres

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// watchedContractRepo implements store.WatchedContractRepository using Postgres.
type watchedContractRepo struct {
	q querier
}

// Insert adds a new watched contract record.
func (r *watchedContractRepo) Insert(ctx context.Context, contract *cca.WatchedContract) error {
	_, err := r.q.Exec(ctx,
		`INSERT INTO watched_contracts (chain_id, address, label, start_block, last_indexed_block)
		 VALUES ($1, $2, $3, $4, $5)`,
		contract.ChainID, contract.Address.Hex(), contract.Label, contract.StartBlock, contract.LastIndexedBlock,
	)
	if err != nil {
		return fmt.Errorf("insert watched contract: %w", err)
	}
	return nil
}

// ListCaughtUp returns addresses of contracts whose last_indexed_block >= globalCursor.
func (r *watchedContractRepo) ListCaughtUp(ctx context.Context, chainID int64, globalCursor uint64) ([]common.Address, error) {
	rows, err := r.q.Query(ctx,
		`SELECT address FROM watched_contracts
		 WHERE chain_id = $1 AND last_indexed_block >= $2`,
		chainID, globalCursor,
	)
	if err != nil {
		return nil, fmt.Errorf("list caught-up contracts: %w", err)
	}
	defer rows.Close()

	var addrs []common.Address
	for rows.Next() {
		var addrHex string
		if err := rows.Scan(&addrHex); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		addrs = append(addrs, common.HexToAddress(addrHex))
	}
	return addrs, rows.Err()
}

// ListNeedingBackfill returns contracts whose last_indexed_block < globalCursor.
func (r *watchedContractRepo) ListNeedingBackfill(ctx context.Context, chainID int64, globalCursor uint64) ([]*cca.WatchedContract, error) {
	rows, err := r.q.Query(ctx,
		`SELECT chain_id, address, label, start_block, last_indexed_block, created_at, updated_at
		 FROM watched_contracts
		 WHERE chain_id = $1 AND last_indexed_block < $2
		 ORDER BY start_block ASC`,
		chainID, globalCursor,
	)
	if err != nil {
		return nil, fmt.Errorf("list needing backfill: %w", err)
	}
	defer rows.Close()

	var contracts []*cca.WatchedContract
	for rows.Next() {
		var c cca.WatchedContract
		var addrHex string
		if err := rows.Scan(&c.ChainID, &addrHex, &c.Label, &c.StartBlock, &c.LastIndexedBlock, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan watched contract: %w", err)
		}
		c.Address = common.HexToAddress(addrHex)
		contracts = append(contracts, &c)
	}
	return contracts, rows.Err()
}

// UpdateLastIndexedBlock advances the per-contract cursor.
func (r *watchedContractRepo) UpdateLastIndexedBlock(ctx context.Context, chainID int64, address string, lastIndexedBlock uint64) error {
	_, err := r.q.Exec(ctx,
		`UPDATE watched_contracts SET last_indexed_block = $1, updated_at = now()
		 WHERE chain_id = $2 AND address = $3`,
		lastIndexedBlock, chainID, address,
	)
	if err != nil {
		return fmt.Errorf("update last indexed block: %w", err)
	}
	return nil
}

// RollbackCursors resets cursors for contracts that were ahead of fromBlock.
func (r *watchedContractRepo) RollbackCursors(ctx context.Context, chainID int64, fromBlock uint64) error {
	_, err := r.q.Exec(ctx,
		`UPDATE watched_contracts SET last_indexed_block = $1 - 1, updated_at = now()
		 WHERE chain_id = $2 AND last_indexed_block >= $1`,
		fromBlock, chainID,
	)
	if err != nil {
		return fmt.Errorf("rollback cursors: %w", err)
	}
	return nil
}
