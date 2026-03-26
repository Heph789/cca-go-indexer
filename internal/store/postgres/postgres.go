package postgres

import (
	"context"
	"fmt"

	"github.com/cca/go-indexer/internal/store"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier defines the subset of pgx methods shared by pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// pgStore implements store.Store backed by PostgreSQL.
type pgStore struct {
	pool *pgxpool.Pool
	tx   pgx.Tx
}

// New connects to PostgreSQL, runs migrations, and returns a store.Store.
func New(ctx context.Context, databaseURL string) (store.Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pool.Ping: %w", err)
	}

	if err := runMigrations(databaseURL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("runMigrations: %w", err)
	}

	return &pgStore{pool: pool}, nil
}

// runMigrations applies embedded SQL migrations using golang-migrate.
func runMigrations(databaseURL string) error {
	source, err := iofs.New(store.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// q returns the active querier — the transaction if inside WithTx, the pool otherwise.
func (s *pgStore) q() querier {
	if s.tx != nil {
		return s.tx
	}
	return s.pool
}

// WithTx begins a transaction, calls fn with a transactional store, and
// commits on nil error or rolls back otherwise.
func (s *pgStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txStore := &pgStore{pool: s.pool, tx: tx}

	if err := fn(txStore); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// Close releases the connection pool.
func (s *pgStore) Close() {
	s.pool.Close()
}

// AuctionRepo returns the auction repository.
func (s *pgStore) AuctionRepo() store.AuctionRepository {
	return &auctionRepo{q: s.q()}
}

// RawEventRepo returns the raw event repository.
func (s *pgStore) RawEventRepo() store.RawEventRepository {
	return &rawEventRepo{q: s.q()}
}

// CursorRepo returns the cursor repository.
func (s *pgStore) CursorRepo() store.CursorRepository {
	return &cursorRepo{q: s.q()}
}

// BlockRepo returns the block repository.
func (s *pgStore) BlockRepo() store.BlockRepository {
	return &blockRepo{q: s.q()}
}
