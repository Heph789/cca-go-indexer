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

// idleInTxTimeout is the per-connection idle_in_transaction_session_timeout.
// Postgres will terminate any session that sits idle inside an open transaction
// for longer than this duration, preventing long-running idle transactions from
// holding locks and blocking other operations.
const idleInTxTimeout = "30s"

// New creates a Postgres-backed store.Store. Every connection in the pool is
// configured with idle_in_transaction_session_timeout via an AfterConnect
// callback so the setting applies even to newly created connections.
func New(ctx context.Context, databaseURL string) (store.Store, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig: %w", err)
	}

	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET idle_in_transaction_session_timeout = '"+idleInTxTimeout+"'")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.NewWithConfig: %w", err)
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
	defer m.Close()

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

// Ping checks database connectivity by sending a ping to the underlying pool.
func (s *pgStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *pgStore) Close() {
	s.pool.Close()
}

func (s *pgStore) AuctionRepo() store.AuctionRepository {
	return &auctionRepo{q: s.q()}
}

// BidRepo returns a stub BidRepository. The backing table is created in a
// later migration (#100); until then callers must not invoke its methods.
func (s *pgStore) BidRepo() store.BidRepository {
	return nil
}

func (s *pgStore) CheckpointRepo() store.CheckpointRepository {
	return &checkpointRepo{q: s.q()}
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

func (s *pgStore) WatchedContractRepo() store.WatchedContractRepository {
	return &watchedContractRepo{q: s.q()}
}

// RollbackFromBlock deletes all indexed data at or after fromBlock across all
// domain repositories and resets watched contract cursors. Used during reorg recovery.
func (s *pgStore) RollbackFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	q := s.q()

	if _, err := q.Exec(ctx, "DELETE FROM raw_events WHERE chain_id = $1 AND block_number >= $2", chainID, fromBlock); err != nil {
		return fmt.Errorf("delete raw events: %w", err)
	}
	if _, err := q.Exec(ctx, "DELETE FROM event_ccaf_auction_created WHERE chain_id = $1 AND block_number >= $2", chainID, fromBlock); err != nil {
		return fmt.Errorf("delete auctions: %w", err)
	}
	if err := s.CheckpointRepo().DeleteFromBlock(ctx, chainID, fromBlock); err != nil {
		return fmt.Errorf("delete checkpoints: %w", err)
	}
	if err := s.WatchedContractRepo().RollbackCursors(ctx, chainID, fromBlock); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, "DELETE FROM indexed_blocks WHERE chain_id = $1 AND block_number >= $2", chainID, fromBlock); err != nil {
		return fmt.Errorf("delete blocks: %w", err)
	}
	return nil
}
