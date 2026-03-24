// Package postgres implements the store interfaces using PostgreSQL via pgx.
//
// The implementation uses a connection pool (pgxpool.Pool) for normal
// operations and swaps in a pgx.Tx when running inside WithTx, so all
// sub-repositories transparently participate in the same transaction.
package postgres

import (
	"context"
	"fmt"

	"github.com/cca/go-indexer/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier is the common interface satisfied by both *pgxpool.Pool and pgx.Tx.
// All repository methods use querier instead of concrete types so they work
// identically inside and outside a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store implements store.Store backed by PostgreSQL.
//
// The pool field always points to the connection pool (needed by WithTx to
// start new transactions). The db field is either the pool (normal mode) or
// a pgx.Tx (inside WithTx), and is what all repos use for queries.
type Store struct {
	pool *pgxpool.Pool
	db   querier
}

// New creates a new Store, connects to PostgreSQL, and runs migrations.
func New(ctx context.Context, connString string) (*Store, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	s := &Store{pool: pool, db: pool}

	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// migrate runs CREATE TABLE IF NOT EXISTS for all tables. This is a simple
// approach suitable for early development — a migration framework (e.g. goose)
// should replace this once the schema stabilizes and needs versioned migrations.
func (s *Store) migrate(ctx context.Context) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS cursors (
			chain_id BIGINT PRIMARY KEY,
			block_number BIGINT NOT NULL DEFAULT 0,
			block_hash TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS blocks (
			chain_id BIGINT NOT NULL,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			parent_hash TEXT NOT NULL,
			PRIMARY KEY (chain_id, block_number)
		)`,
		`CREATE TABLE IF NOT EXISTS raw_events (
			chain_id BIGINT NOT NULL,
			block_number BIGINT NOT NULL,
			block_hash TEXT NOT NULL,
			tx_hash TEXT NOT NULL,
			log_index INT NOT NULL,
			address TEXT NOT NULL,
			event_name TEXT NOT NULL,
			topics_json TEXT NOT NULL,
			data_hex TEXT NOT NULL,
			decoded_json TEXT NOT NULL,
			indexed_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (chain_id, block_number, log_index)
		)`,
		`CREATE TABLE IF NOT EXISTS auctions (
			auction_address TEXT NOT NULL,
			token TEXT NOT NULL,
			total_supply NUMERIC NOT NULL,
			currency TEXT NOT NULL,
			tokens_recipient TEXT NOT NULL,
			funds_recipient TEXT NOT NULL,
			start_block BIGINT NOT NULL,
			end_block BIGINT NOT NULL,
			claim_block BIGINT NOT NULL,
			tick_spacing_q96 NUMERIC NOT NULL,
			validation_hook TEXT NOT NULL,
			floor_price_q96 NUMERIC NOT NULL,
			required_currency_raised NUMERIC NOT NULL,
			auction_steps_data BYTEA NOT NULL,
			emitter_contract TEXT NOT NULL,
			chain_id BIGINT NOT NULL,
			block_number BIGINT NOT NULL,
			tx_hash TEXT NOT NULL,
			log_index INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (chain_id, block_number, log_index)
		)`,
	}

	for _, ddl := range migrations {
		if _, err := s.db.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// Close closes the underlying connection pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// WithTx runs fn inside a database transaction. If fn returns nil the
// transaction is committed; otherwise it is rolled back.
//
// The txStore passed to fn is a new Store whose db field points to the
// transaction. Any repos obtained from txStore (e.g. txStore.AuctionRepo())
// will execute queries within the same tx, ensuring atomicity across all
// writes in a single tick.
func (s *Store) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	// Create a transactional Store that shares the pool (for nested WithTx,
	// though currently unused) but routes all queries through the tx.
	txStore := &Store{pool: s.pool, db: tx}

	if err := fn(txStore); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}

// AuctionRepo returns the auction repository.
func (s *Store) AuctionRepo() store.AuctionRepository {
	return &AuctionRepo{db: s.db}
}

// RawEventRepo returns the raw event repository.
func (s *Store) RawEventRepo() store.RawEventRepository {
	return &RawEventRepo{db: s.db}
}

// CursorRepo returns the cursor repository.
func (s *Store) CursorRepo() store.CursorRepository {
	return &CursorRepo{db: s.db}
}

// BlockRepo returns the block repository.
func (s *Store) BlockRepo() store.BlockRepository {
	return &BlockRepo{db: s.db}
}
