// Package postgres implements the store interfaces using PostgreSQL via pgx.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/cca/go-indexer/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// querier is the common interface satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store implements store.Store backed by PostgreSQL.
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

func (s *Store) migrate(ctx context.Context) error {
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("fs.Sub: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS)
	if err != nil {
		return fmt.Errorf("goose new provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
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
func (s *Store) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

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
