// Package postgres implements the store interfaces using pgx and pgxpool.
// Migrations are embedded in the binary and run at startup.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cca/go-indexer/internal/store"
)

// querier is the common interface between pgxpool.Pool and pgx.Tx,
// allowing repository methods to work with either.
type querier interface {
	// Placeholder — both pgxpool.Pool and pgx.Tx satisfy this via
	// Query, QueryRow, Exec methods. The exact interface will be
	// defined during implementation.
}

// pgStore implements store.Store.
// When tx is non-nil, all operations use the transaction.
// When tx is nil, operations go directly to the pool.
type pgStore struct {
	pool *pgxpool.Pool
	tx   pgx.Tx // non-nil inside a WithTx callback
}

// New creates a new postgres-backed Store.
// It connects to the database, runs pending migrations, and returns
// a ready-to-use Store.
func New(ctx context.Context, databaseURL string) (store.Store, error) {
	// TODO:
	// 1. pgxpool.New(ctx, databaseURL)
	// 2. pool.Ping(ctx) to verify connectivity
	// 3. runMigrations(databaseURL) using golang-migrate
	// 4. Return &pgStore{pool: pool}
	panic("not implemented")
}

// runMigrations applies pending database migrations using golang-migrate.
// Migrations are embedded in the binary via //go:embed.
func runMigrations(databaseURL string) error {
	// TODO:
	// 1. Create iofs source from embedded migrations directory
	// 2. Create postgres driver from databaseURL
	// 3. migrate.NewWithInstance(...)
	// 4. m.Up() — no-op if already current
	panic("not implemented")
}

func (s *pgStore) AuctionRepo() store.AuctionRepository {
	return &auctionRepo{q: s.querier()}
}

func (s *pgStore) RawEventRepo() store.RawEventRepository {
	return &rawEventRepo{q: s.querier()}
}

func (s *pgStore) CursorRepo() store.CursorRepository {
	return &cursorRepo{q: s.querier()}
}

func (s *pgStore) BlockRepo() store.BlockRepository {
	return &blockRepo{q: s.querier()}
}

// WithTx executes fn inside a single database transaction.
// The txStore provided to fn shares the same underlying pgx.Tx,
// so all repository operations are atomic.
func (s *pgStore) WithTx(ctx context.Context, fn func(store.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txStore := &pgStore{pool: s.pool, tx: tx}
	if err := fn(txStore); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *pgStore) Close() {
	s.pool.Close()
}

// querier returns the active transaction if inside WithTx,
// otherwise returns the connection pool.
func (s *pgStore) querier() querier {
	if s.tx != nil {
		return s.tx
	}
	return s.pool
}
