# Chunk 1: Simplest Viable Indexer Loop

Goal: a working indexer loop with all real dependencies, no optimizations.
Builds on the scaffold at branch `indexer-1-/scaffold-1`.

## Sub-chunks

### 1a — Loop skeleton + batch calc
**Branch:** `indexer-1-/simple-loop-1-/loop-skeleton`
**PR:** 1 of 5

Code:
- `ChainIndexer` struct, `New()`, `Run()`, `IndexerConfig`
- Batch calc inline: safeHead (with underflow guard), range capping, nothing-to-do check
- Hand-written fakes for `eth.Client`, `store.Store` + sub-repos, `HandlerRegistry`
- Reorg detection stubbed (always "no reorg")

Tests:
- Batch range: chain head 100, cursor 50, batchSize 10 → FilterLogs called with 51-60
- SafeHead: confirmations applied, underflow guard when head < confirmations
- Nothing to do: cursor at safeHead → no FilterLogs, sleeps
- Catch-up: cursor far behind → re-polls immediately (no sleep)
- Dispatch: logs returned → HandleLog called for each
- Atomicity: block hashes + cursor upsert inside WithTx
- Shutdown: context cancellation exits Run() cleanly
- First run: no cursor in DB → starts from StartBlock config

### 1b — Handler registry + AuctionCreated handler
**Branch:** `indexer-1-/simple-loop-1-/handlers`
**PR:** 2 of 5

Code:
- `HandlerRegistry` with topic0-based dispatch
- `AuctionCreatedHandler` that decodes a log into `Auction` + `RawEvent`

Tests (faked store):
- Registry returns correct topic filter
- Registry dispatches to correct handler by topic0
- Registry panics on duplicate registration
- Handler decodes synthetic `types.Log` → correct `Auction` fields
- Handler inserts both `RawEvent` and `Auction`

### 1c — Postgres store
**Branch:** `indexer-1-/simple-loop-1-/postgres-store`
**PR:** 3 of 5

Code:
- `postgres.New()` with pgxpool + migrations
- All repos: CursorRepo, BlockRepo, RawEventRepo, AuctionRepo
- `WithTx` implementation

Tests (testcontainers):
- Cursor: Get returns zero on empty, Upsert + Get round-trip
- Block: Insert + GetHash, DeleteFrom
- RawEvent: Insert, DeleteFromBlock
- Auction: Insert, DeleteFromBlock
- WithTx: commit on success, rollback on error

### 1d — Eth client
**Branch:** `indexer-1-/simple-loop-1-/eth-client`
**PR:** 4 of 5

Code:
- Thin wrapper around go-ethereum `ethclient.Client`
- Satisfies `eth.Client` interface (BlockNumber, HeaderByNumber, FilterLogs, Close)

Tests:
- Verify interface satisfaction at compile time

### 1e — Config + main wiring
**Branch:** `indexer-1-/simple-loop-1-/main-wiring`
**PR:** 5 of 5

Code:
- `config.Load()` from environment variables
- Validation (required fields, defaults)
- `main.go` wires real deps: config → eth client → store → registry → indexer
- Contract addresses from config (not DB)

Tests:
- Config loads from env, applies defaults
- Required fields error when missing

## TODOs (out of scope)
- Retry transport (exponential backoff on 429/5xx)
- DB-driven contract addresses (`WatchedContractRepository`)
- Reorg detection & rollback (chunk 2)

## Principles
- TDD: tests first, simplest implementation to pass
- Hand-written fakes (no mock library)
- One PR per sub-chunk
- No optimizations — ship simple, iterate later
