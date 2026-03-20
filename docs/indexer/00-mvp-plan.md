# Indexer MVP Implementation Plan

| | |
|---|---|
| **Scope** | Index `AuctionCreated` events from the CCA factory contract on Sepolia. No API — just the indexer process writing to PostgreSQL. |
| **Goal** | Working end-to-end pipeline (RPC → parse → store) with reorg handling, built so adding new events and chains is mechanical. |
| **Based on** | [Planning Summary](../init-planning/00-summary.md) phases 0–4 |

---

## Directory Structure

```
cca-go-indexer/
├── cmd/
│   └── indexer/
│       └── main.go                    # entrypoint: config, wiring, run
├── internal/
│   ├── config/
│   │   └── config.go                  # Config struct, Load() from env
│   ├── domain/
│   │   ├── chain.go                   # ChainID type, chain constants
│   │   └── cca/
│   │       ├── auction.go             # Auction domain type
│   │       └── event.go               # RawEvent domain type
│   ├── eth/
│   │   ├── client.go                  # RPC client interface + implementation
│   │   └── abi/
│   │       ├── factory.json           # factory contract ABI (embedded)
│   │       └── factory.go             # parsed ABI, event ID constants
│   ├── indexer/
│   │   ├── indexer.go                 # ChainIndexer: poll loop, cursor mgmt
│   │   ├── handler.go                 # EventHandler interface + HandlerRegistry
│   │   ├── reorg.go                   # reorg detection + rollback
│   │   └── handlers/
│   │       └── auction_created.go     # AuctionCreated handler
│   └── store/
│       ├── store.go                   # Store interface (repos + WithTx)
│       ├── postgres/
│       │   ├── postgres.go            # pool, migrations, constructor
│       │   ├── txn.go                 # transaction helper
│       │   ├── auction.go             # AuctionRepository impl
│       │   ├── raw_event.go           # RawEventRepository impl
│       │   ├── cursor.go              # CursorRepository impl
│       │   └── block.go               # BlockRepository impl
│       └── migrations/
│           ├── 000001_init.up.sql
│           └── 000001_init.down.sql
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── go.mod
└── CLAUDE.md
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/ethereum/go-ethereum` | ABI parsing, types, `ethclient` RPC |
| `github.com/jackc/pgx/v5` | PostgreSQL driver + `pgxpool` connection pooling |
| `github.com/golang-migrate/migrate/v4` | Database migrations (with `iofs` source, `pgx5` driver) |
| `github.com/testcontainers/testcontainers-go` | Integration tests (test-only) |

No ORM, no sqlc — hand-written SQL for MVP simplicity. No external config library — `os.Getenv` with defaults.

---

## Config

**File:** `internal/config/config.go`

```go
type Config struct {
    DatabaseURL    string        // DATABASE_URL
    RPCURL         string        // RPC_URL
    ChainID        int64         // CHAIN_ID (11155111 for Sepolia)
    FactoryAddr    string        // FACTORY_ADDRESS
    StartBlock     uint64        // START_BLOCK (factory deploy block)
    PollInterval   time.Duration // POLL_INTERVAL (default 12s)
    BlockBatchSize uint64        // BLOCK_BATCH_SIZE (default 100)
    MaxBlockRange  uint64        // MAX_BLOCK_RANGE (default 2000)
    Confirmations  uint64        // CONFIRMATIONS (default 0)
    LogLevel       string        // LOG_LEVEL (default "info")
    LogFormat      string        // LOG_FORMAT ("json" | "text")
}
```

`Load()` reads from env, validates required fields (`DatabaseURL`, `RPCURL`, `ChainID`, `FactoryAddr`), returns error on missing.

---

## Domain Types

### `internal/domain/cca/auction.go`

```go
type Auction struct {
    AuctionAddress common.Address
    TokenOut       common.Address
    CurrencyIn     common.Address
    Owner          common.Address
    StartTime      uint64
    EndTime        uint64
    // additional fields TBD from ABI

    ChainID     int64
    BlockNumber uint64
    TxHash      common.Hash
    LogIndex    uint
    CreatedAt   time.Time
}
```

### `internal/domain/cca/event.go`

```go
type RawEvent struct {
    ChainID     int64
    BlockNumber uint64
    BlockHash   common.Hash
    TxHash      common.Hash
    LogIndex    uint
    Address     common.Address
    EventName   string
    TopicsJSON  string
    DataHex     string
    DecodedJSON string
    IndexedAt   time.Time
}
```

Domain types are plain structs — no DB tags, no JSON tags. Mapping to/from DB rows is the store layer's job.

### `internal/domain/chain.go`

```go
type ChainID = int64

const (
    ChainMainnet  ChainID = 1
    ChainBase     ChainID = 8453
    ChainUnichain ChainID = 130
    ChainSepolia  ChainID = 11155111
)
```

---

## Database Schema

**Migration:** `000001_init.up.sql`

```sql
CREATE TABLE indexer_cursors (
    chain_id        BIGINT NOT NULL,
    last_block      BIGINT NOT NULL,
    last_block_hash TEXT   NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id)
);

CREATE TABLE raw_events (
    chain_id     BIGINT  NOT NULL,
    block_number BIGINT  NOT NULL,
    block_hash   TEXT    NOT NULL,
    tx_hash      TEXT    NOT NULL,
    log_index    INTEGER NOT NULL,
    address      TEXT    NOT NULL,
    event_name   TEXT    NOT NULL,
    topics       JSONB   NOT NULL,
    data         TEXT    NOT NULL,
    decoded      JSONB,
    indexed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number, tx_hash, log_index)
);

CREATE INDEX idx_raw_events_block ON raw_events (chain_id, block_number);
CREATE INDEX idx_raw_events_event ON raw_events (chain_id, event_name);

CREATE TABLE auctions (
    chain_id        BIGINT NOT NULL,
    auction_address TEXT   NOT NULL,
    token_out       TEXT   NOT NULL,
    currency_in     TEXT   NOT NULL,
    owner           TEXT   NOT NULL,
    start_time      BIGINT NOT NULL,
    end_time        BIGINT NOT NULL,
    block_number    BIGINT NOT NULL,
    tx_hash         TEXT   NOT NULL,
    log_index       INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, auction_address)
);

CREATE TABLE indexed_blocks (
    chain_id     BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   TEXT   NOT NULL,
    parent_hash  TEXT   NOT NULL,
    indexed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number)
);
```

**Design notes:**
- Addresses as `TEXT` (hex with 0x prefix) — simpler for debugging than `BYTEA`
- `(chain_id, block_number, tx_hash, log_index)` composite PK guarantees idempotent inserts
- `indexed_blocks` stores per-block hashes for reorg detection
- All tables have `chain_id` for multi-chain readiness

---

## Store Layer

### `internal/store/store.go` — Interfaces

```go
type Store interface {
    AuctionRepo() AuctionRepository
    RawEventRepo() RawEventRepository
    CursorRepo() CursorRepository
    BlockRepo() BlockRepository
    WithTx(ctx context.Context, fn func(Store) error) error
    Close()
}

type AuctionRepository interface {
    Insert(ctx context.Context, auction *cca.Auction) error
    DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

type RawEventRepository interface {
    Insert(ctx context.Context, event *cca.RawEvent) error
    DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error
}

type CursorRepository interface {
    Get(ctx context.Context, chainID int64) (blockNumber uint64, blockHash string, err error)
    Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error
}

type BlockRepository interface {
    Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error
    GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error)
    DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error
}
```

### `WithTx` — Atomic Block Processing

The indexer calls `store.WithTx(ctx, func(txStore Store) error { ... })`. All repository operations inside use the same postgres transaction. If anything fails, the entire block's writes roll back.

### Postgres Implementation

```go
type pgStore struct {
    pool *pgxpool.Pool
    tx   pgx.Tx // non-nil inside a transaction
}

func (s *pgStore) querier() querier {
    if s.tx != nil { return s.tx }
    return s.pool
}
```

All `INSERT` statements use `ON CONFLICT DO NOTHING` for idempotency. Migrations embedded via `//go:embed` and run at startup through `golang-migrate`.

---

## Eth Client

### `internal/eth/client.go` — Interface

```go
type Client interface {
    BlockNumber(ctx context.Context) (uint64, error)
    BlockByNumber(ctx context.Context, number uint64) (BlockHeader, error)
    FilterLogs(ctx context.Context, query FilterQuery) ([]types.Log, error)
    Close()
}

type BlockHeader struct {
    Number     uint64
    Hash       common.Hash
    ParentHash common.Hash
    Timestamp  uint64
}

type FilterQuery struct {
    FromBlock uint64
    ToBlock   uint64
    Addresses []common.Address
    Topics    [][]common.Hash
}
```

Wraps `ethclient.Client`. Interface exists for test mocking. MVP includes basic retry with exponential backoff on transient errors (connection reset, 429).

### `internal/eth/abi/factory.go` — ABI Embedding

```go
//go:embed factory.json
var factoryABIJSON []byte

var FactoryABI abi.ABI
var AuctionCreatedEventID common.Hash

func init() {
    parsed, _ := abi.JSON(bytes.NewReader(factoryABIJSON))
    FactoryABI = parsed
    AuctionCreatedEventID = parsed.Events["AuctionCreated"].ID
}
```

**ABI source:** Obtain from the [CCA contract repo](https://github.com/Uniswap/continuous-clearing-auction) or verified Sepolia explorer. Only the `AuctionCreated` event definition is needed for MVP.

---

## Event Processing — Extensibility Core

### `internal/indexer/handler.go`

```go
type EventHandler interface {
    EventName() string
    EventID() common.Hash
    Handle(ctx context.Context, chainID int64, log types.Log, store store.Store) error
}

type HandlerRegistry struct {
    handlers map[common.Hash]EventHandler
}

func NewRegistry(handlers ...EventHandler) *HandlerRegistry
func (r *HandlerRegistry) TopicFilter() [][]common.Hash  // for eth_getLogs
func (r *HandlerRegistry) HandleLog(ctx context.Context, chainID int64, log types.Log, s store.Store) error
```

**Why this pattern:** Adding a new event = one new file implementing `EventHandler` + registering it in `main.go`. No changes to the indexer loop, registry, or reorg handling.

### `internal/indexer/handlers/auction_created.go`

```go
func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
    // 1. Decode log using factory ABI
    // 2. Map to domain cca.Auction
    // 3. s.RawEventRepo().Insert(...)  — raw event for audit trail
    // 4. s.AuctionRepo().Insert(...)   — derived auction record
}
```

---

## Indexer Loop

### `internal/indexer/indexer.go`

```go
type ChainIndexer struct {
    chainID   int64
    ethClient eth.Client
    store     store.Store
    registry  *HandlerRegistry
    config    IndexerConfig
    logger    *slog.Logger
}
```

**Run loop pseudocode:**

```
1. Load cursor from DB (or use StartBlock if none)
2. Loop (until context cancelled):
   a. Get chain head from RPC
   b. safeHead = chainHead - confirmations
   c. If cursor >= safeHead → sleep PollInterval, continue
   d. from = cursor + 1, to = min(cursor + batchSize, safeHead)
   e. Check for reorg at from-1 (compare stored hash vs chain)
   f. If reorg → rollback, reset cursor, continue
   g. FilterLogs(from, to, factoryAddr, registry.TopicFilter())
   h. Fetch block headers for hash tracking
   i. store.WithTx: {
        for each log → registry.HandleLog(...)
        for each block → blockRepo.Insert(...)
        cursorRepo.Upsert(chainID, to, lastBlockHash)
      }
   j. If to < safeHead → continue immediately (catching up)
      else → sleep PollInterval
```

**Key properties:**
- **Atomic** — all writes per block range in one DB transaction
- **Resumable** — cursor in DB, restarts pick up where they left off
- **Backfill-friendly** — no sleep between batches when behind head

### `cmd/indexer/main.go`

```
1. Load config
2. Set up slog logger
3. Connect to postgres (pgxpool)
4. Run migrations
5. Create eth client
6. Create store
7. Create handlers → registry
8. Create ChainIndexer
9. Handle OS signals → cancel context
10. indexer.Run(ctx)
```

---

## Reorg Handling

### `internal/indexer/reorg.go`

**Detection:** Before processing a new range, verify the parent hash of the first new block matches the stored hash of the previous block.

**Rollback:**

```
1. Walk backwards to find common ancestor:
   for block = reorgBlock; block > 0; block-- {
       if storedHash == chainHash → common ancestor found
   }
2. Atomic rollback in one transaction:
   rawEventRepo.DeleteFromBlock(chainID, ancestor+1)
   auctionRepo.DeleteFromBlock(chainID, ancestor+1)
   blockRepo.DeleteFrom(chainID, ancestor+1)
   cursorRepo.Upsert(chainID, ancestor, ancestorHash)
3. Return ancestor as new cursor position
```

**Safety:** Max reorg depth of 128 blocks. If deeper, log error and halt.

---

## Testing Strategy

### Unit Tests
- `config/` — env var loading, defaults, validation
- `indexer/handlers/` — event parsing from `types.Log` to domain types (mock store)
- `indexer/handler.go` — registry dispatch, topic filter construction
- `indexer/reorg.go` — reorg detection logic (mock eth client + store)
- `indexer/indexer.go` — loop behavior: catch-up, sleep at head, reorg handling (mocks)

Mock approach: interfaces for `eth.Client` and `store.Store` enable manual mock implementations.

### Integration Tests (`//go:build integration`)
- Repository CRUD operations
- Idempotent insert behavior (same event twice → no error, single row)
- `WithTx` atomicity (partial failure → full rollback)
- `DeleteFromBlock` for reorg simulation
- Mini end-to-end: mock eth client with canned logs → real postgres → verify stored data

Uses `testcontainers-go` PostgreSQL module to spin up a real database per test.

---

## Extensibility Guide

### Adding a new event (e.g. BidSubmitted)

1. Domain type → `internal/domain/cca/bid.go`
2. Migration → `000002_add_bids.up.sql`
3. Repository interface → add `BidRepository` to `store.go`
4. Repository impl → `internal/store/postgres/bid.go`
5. Handler → `internal/indexer/handlers/bid_submitted.go` implementing `EventHandler`
6. Wire → register handler in `main.go`

No changes to the indexer loop, registry, or reorg handling.

### Adding a new chain

1. Config: add second set of env vars (or config array)
2. Wire: create second `ChainIndexer`, run as goroutine alongside first
3. Database: same DB, `chain_id` discriminator handles separation

No code changes to any internal package.

### Adding auction-level events (from auction contracts, not factory)

Post-MVP: when `AuctionCreated` is processed, the auction address becomes known. A "dynamic address tracker" would add auction addresses to the `FilterLogs` watch list. The `HandlerRegistry` already dispatches by topic (not source contract), so auction-level handlers work without changes to the dispatch pattern.

---

## Build Order

| Step | Scope | Depends on | Validates with |
|---|---|---|---|
| 1 | `go.mod`, `Makefile`, `Dockerfile`, `docker-compose.yml`, `.env.example` | — | `go mod tidy` |
| 2 | `internal/config/` + tests | 1 | `go test ./internal/config/...` |
| 3 | `internal/domain/` (types only) | 1 | compiles |
| 4 | Migrations (`000001_init`) | 3 | apply to local postgres |
| 5 | `internal/store/store.go` (interfaces) | 3 | compiles |
| 6 | `internal/store/postgres/` (implementation) | 4, 5 | integration tests |
| 7 | `internal/eth/abi/` (factory ABI + parsing) | 1 | event ID hash matches |
| 8 | `internal/eth/client.go` | 7 | compiles |
| 9 | `internal/indexer/handler.go` (interface + registry) | 3, 5 | unit tests |
| 10 | `internal/indexer/handlers/auction_created.go` | 7, 9 | unit tests with mock store |
| 11 | `internal/indexer/reorg.go` | 5, 8 | unit tests with mocks |
| 12 | `internal/indexer/indexer.go` | 8, 9, 11 | unit tests with mocks |
| 13 | `cmd/indexer/main.go` | all above | `make build`, manual run vs Sepolia |
| 14 | `CLAUDE.md` | all above | — |

---

## Open Questions

1. **ABI source** — Need exact `AuctionCreated` event ABI from the CCA contract repo or Sepolia block explorer. Handler code shape is stable regardless of exact fields.
2. **Start block** — Determine the Sepolia block at which the factory (`0xCCccCcCAE7503Cac057829BF2811De42E16e0bD5`) was deployed.
3. **Go version** — Use Go 1.22+ for `log/slog` stdlib support.
4. **Auction fields** — The `Auction` struct fields beyond address/token/currency/owner/times depend on what `AuctionCreated` actually emits. Adapt once ABI is obtained.
