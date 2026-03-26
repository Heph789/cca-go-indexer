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

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type pgStore struct {
	pool *pgxpool.Pool
	tx   pgx.Tx
}

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

func (s *pgStore) q() querier {
	if s.tx != nil {
		return s.tx
	}
	return s.pool
}

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

func (s *pgStore) Close() {
	s.pool.Close()
}

func (s *pgStore) AuctionRepo() store.AuctionRepository {
	return &auctionRepo{q: s.q()}
}

func (s *pgStore) RawEventRepo() store.RawEventRepository {
	return &rawEventRepo{q: s.q()}
}

func (s *pgStore) CursorRepo() store.CursorRepository {
	return &cursorRepo{q: s.q()}
}

func (s *pgStore) BlockRepo() store.BlockRepository {
	return &blockRepo{q: s.q()}
}
