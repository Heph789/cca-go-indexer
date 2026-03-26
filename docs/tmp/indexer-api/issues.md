# Indexer + API — Implementation Issues

## Parent: Implement CCA indexer and API from scaffold

### Description / Requirements
Implement the full CCA event indexer and read-only REST API from the scaffold on branch `indexer-api-happy-1-/scaffold-1`. The system indexes `AuctionCreated` events from the CCA factory contract, stores them in PostgreSQL, and serves them via a JSON API.

Issues are organized in three phases, each followed by an automated QA gate:
1. **Happy path** (1–8): Get bytes flowing end-to-end — indexer writes, API reads, everything connects.
2. **Resilience** (9–12): Retry logic, reorg handling, error recovery.
3. **Production readiness** (13–17): Middleware, caching, readiness probes, config validation.

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`

---

# Phase 1 — Happy Path

The minimum to get the indexer writing events to Postgres and the API serving them. No retries, no reorg handling, no middleware beyond routing. Mock-first for consumers, then implement the concrete dependencies.

---

## 1. Indexer loop: happy-path batch processing

### Description / Requirements
Test the happy path of `ChainIndexer.Run` — load cursor, poll chain head, fetch logs, dispatch to handlers, record block hashes, advance cursor. All dependencies mocked. Skip reorg detection and retry logic for now (assume no reorgs, no errors). Also test `HandlerRegistry` dispatch and `TopicFilter` since the loop depends on them directly.

### Branch name
`indexer-api-happy-1-/indexer-loop-happy-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/indexer/indexer.go` — `ChainIndexer.Run()`
- `internal/indexer/handler.go` — `HandlerRegistry`, `EventHandler` interface
- `internal/eth/client.go` — `Client` interface (mock target)
- `internal/store/store.go` — `Store` interface and sub-repositories (mock targets)

### Unit Tests (TDD)
- `HandlerRegistry` dispatches logs to the correct handler by topic0
- `HandlerRegistry.TopicFilter` returns all registered event IDs
- `HandleLog` returns error for log with no topics
- `HandleLog` returns error for unregistered topic
- `NewRegistry` panics on duplicate EventID
- Indexer loads cursor from store on startup
- Indexer uses `StartBlock - 1` as cursor when store returns `(0, "", nil)`
- Indexer fetches logs for the correct block range `[cursor+1, min(cursor+batchSize, safeHead)]`
- Indexer dispatches fetched logs through the handler registry
- Indexer inserts block hashes for each block in the batch
- Indexer advances cursor to end of batch after successful processing
- Indexer sleeps when cursor >= safeHead (at chain head)
- Indexer applies `Confirmations` buffer (safeHead = chainHead - confirmations)
- Indexer stops when context is cancelled
- All writes (logs, blocks, cursor) happen inside a single `WithTx` call

### Integration Tests
None — all dependencies are mocked.

### Dependencies
None — all interfaces are mocked.

### Blocked by
Nothing

### Principles
- Atomic batches: all events + block hashes + cursor in one DB transaction.
- Resumable: cursor in DB, restart picks up from last committed position.
- Interface-driven: all dependencies mocked.

---

## 2. AuctionCreated event handler (ABI decode + persist)

### Description / Requirements
Implement `AuctionCreatedHandler.Handle` — decode `AuctionCreated` log using the factory ABI, build the `RawEvent` with serialized topics/data/decoded JSON, build the typed `Auction` from decoded fields, and insert both. Test against mock store repos. Indexed fields come from `log.Topics[1]` (auction) and `log.Topics[2]` (token). Non-indexed fields (`amount`, `configData`) are ABI-decoded from `log.Data`. `configData` is further ABI-decoded into `AuctionParameters` fields.

### Branch name
`indexer-api-happy-1-/auction-handler-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/indexer/handlers/auction_created.go` — `Handle()`
- `internal/eth/abi/factory.go` — `FactoryABI`, `AuctionCreatedEventID`
- `internal/eth/abi/ContinuousClearingAuctionFactory.json` — ABI JSON
- `internal/domain/cca/auction.go` — `Auction` struct
- `internal/domain/cca/event.go` — `RawEvent` struct

### Unit Tests (TDD)
- `Handle` decodes indexed fields (auction address, token) from `log.Topics`
- `Handle` decodes non-indexed fields (amount, configData) from `log.Data`
- `Handle` decodes configData into all AuctionParameters fields (currency, tokensRecipient, fundsRecipient, startBlock, endBlock, claimBlock, tickSpacing, validationHook, floorPrice, requiredCurrencyRaised)
- `Handle` inserts a raw event with serialized topics (JSON array), hex-encoded data, and decoded JSON
- `Handle` inserts a typed auction with all fields mapped correctly
- `Handle` returns an error if ABI decoding fails (malformed log data)
- `Handle` propagates raw event insert errors
- `Handle` propagates auction insert errors
- `EventName` returns "AuctionCreated"
- `EventID` returns `AuctionCreatedEventID`

### Integration Tests
None — store repos are mocked.

### Dependencies
- `internal/eth/abi` package (already functional — ABI parsing works via `init()`)

### Blocked by
Nothing

### Principles
- Raw + typed storage: every event gets a raw record and a typed record.
- Atomic batches: handler runs inside `WithTx`.

---

## 3. API handler: auction GET

### Description / Requirements
Test the auction `Get` handler against a mock store. The handler is scaffold-complete — validate request parsing, address validation, store interaction, and response mapping. Skip middleware and health probes for now.

### Branch name
`indexer-api-happy-1-/api-auction-get-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/api/handlers/auction.go` — `Get()`, `AuctionResponse`, `toAuctionResponse`, `isValidAddress`
- `internal/api/httputil/response.go` — `WriteJSON`, `WriteError`, `WriteNotFound`
- `internal/store/store.go` — `Store`, `AuctionRepository` interfaces (mock targets)

### Unit Tests (TDD)
- `Get` returns 200 with auction data for a valid address (mock store)
- `Get` returns 404 for a non-existent auction address (store returns nil, nil)
- `Get` returns 400 for an invalid Ethereum address (wrong length, missing 0x)
- `Get` returns 400 when address path param is empty
- `Get` returns 500 when the store returns an error
- `Get` normalizes address to lowercase before querying store
- `toAuctionResponse` maps all domain fields to response fields correctly
- `toAuctionResponse` lowercases all address fields
- `toAuctionResponse` converts `big.Int` fields to string
- `isValidAddress` accepts valid 42-char 0x-prefixed address
- `isValidAddress` rejects too-short, too-long, and non-0x addresses
- Response body wraps data in `{"data": ...}` envelope

### Integration Tests
None — store is mocked.

### Dependencies
None — store is mocked.

### Blocked by
Nothing

### Principles
None specific — straightforward request/response.

---

## 4. Postgres store: connection, migrations, and querier

### Description / Requirements
Implement `postgres.New` (connection pool + migrations) and fix the `querier` interface so repository methods can use both `pgxpool.Pool` and `pgx.Tx`. Run embedded migrations via `golang-migrate`. The `querier` interface needs the actual pgx methods (`QueryRow`, `Query`, `Exec`) that both `pgxpool.Pool` and `pgx.Tx` satisfy.

### Branch name
`indexer-api-happy-1-/pg-store-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/store/postgres/postgres.go` — `New()`, `runMigrations()`, `querier` interface
- `internal/store/migrations/000001_init.up.sql`
- `internal/store/migrations/000001_init.down.sql`
- `go.mod` — `pgx/v5`, `golang-migrate/v4`

### Unit Tests (TDD)
- `New` returns a working store when given a valid `DATABASE_URL`
- `New` returns an error for an invalid `DATABASE_URL`
- `runMigrations` applies the init migration (tables exist after call)
- `runMigrations` is idempotent (calling twice does not error)
- `querier()` returns `tx` when inside `WithTx`, returns `pool` otherwise
- `WithTx` rolls back on error returned from callback
- `WithTx` commits when callback returns nil
- `Close` releases the pool

### Integration Tests
None — this is the store foundation.

### Dependencies
- `github.com/jackc/pgx/v5` (in go.mod)
- `github.com/golang-migrate/migrate/v4` (in go.mod)
- Running PostgreSQL instance for tests

### Blocked by
Nothing

### Principles
- Atomic batches: `WithTx` is the mechanism that ensures all writes in a block range are atomic.

---

## 5. Repository implementations (cursor, block, raw event, auction)

### Description / Requirements
Implement all four repository types. These are straightforward SQL — each method is a single query. Grouped into one issue because they're small, share the same test harness (postgres test helper from issue 4), and are all needed before end-to-end wiring.

- `cursorRepo`: `Get`, `Upsert` — tracks per-chain indexing progress
- `blockRepo`: `Insert`, `GetHash`, `DeleteFrom` — reorg detection storage
- `rawEventRepo`: `Insert`, `DeleteFromBlock` — audit trail
- `auctionRepo`: `Insert`, `DeleteFromBlock`, `GetByAddress` — typed event storage

### Branch name
`indexer-api-happy-1-/repositories-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/store/postgres/cursor.go` — `Get()`, `Upsert()`
- `internal/store/postgres/block.go` — `Insert()`, `GetHash()`, `DeleteFrom()`
- `internal/store/postgres/raw_event.go` — `Insert()`, `DeleteFromBlock()`
- `internal/store/postgres/auction.go` — `Insert()`, `DeleteFromBlock()`, `GetByAddress()`
- `internal/store/store.go` — all repository interfaces
- `internal/domain/cca/auction.go` — `Auction` struct
- `internal/domain/cca/event.go` — `RawEvent` struct

### Unit Tests (TDD)
**Cursor:**
- `Get` returns `(0, "", nil)` when no cursor exists for a chain
- `Upsert` inserts a new cursor and `Get` returns it
- `Upsert` updates an existing cursor (block number and hash change)
- Operations are scoped by `chain_id`

**Block:**
- `Insert` stores a block hash and `GetHash` retrieves it
- `Insert` with duplicate `(chain_id, block_number)` does not error (ON CONFLICT DO NOTHING)
- `GetHash` returns `""` for a non-existent block
- `DeleteFrom` removes blocks at and after the given block number
- `DeleteFrom` does not remove blocks before the given block number

**Raw event:**
- `Insert` stores a raw event and it exists in the table
- `Insert` with duplicate PK does not error
- `DeleteFromBlock` removes events at and after the given block number
- `DeleteFromBlock` does not remove events before the given block number

**Auction:**
- `Insert` stores an auction and `GetByAddress` retrieves it with all fields correct
- `Insert` with duplicate PK does not error
- `GetByAddress` returns `(nil, nil)` for a non-existent address
- `DeleteFromBlock` removes auctions at and after the given block number
- `big.Int` fields round-trip correctly through NUMERIC columns

### Integration Tests
- `WithTx` commit makes all repo writes visible
- `WithTx` rollback discards all repo writes

### Dependencies
- Issue 4 (Postgres store)

### Blocked by
Issue 4 (Postgres store)

### Principles
- Idempotent writes: `ON CONFLICT DO NOTHING` / `ON CONFLICT DO UPDATE`.
- Raw + typed storage: raw_events for audit, typed tables for queries.

---

## 6. Ethereum RPC client (NewClient only)

### Description / Requirements
Implement `eth.NewClient` — dial the RPC endpoint with go-ethereum's `ethclient` using a standard HTTP client. Skip the retry transport for now (use `http.DefaultTransport`). This is the minimum needed to get the indexer talking to a real chain.

### Branch name
`indexer-api-happy-1-/eth-client-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/eth/client.go` — `NewClient()`

### Unit Tests (TDD)
- `NewClient` returns a `Client` that satisfies the interface
- `NewClient` returns an error for an invalid/unreachable RPC URL

### Integration Tests
None — real RPC calls are not part of unit tests.

### Dependencies
- `github.com/ethereum/go-ethereum` (in go.mod)

### Blocked by
Nothing

### Principles
None — minimal wiring.

---

## 7. Config, entry points, and end-to-end wiring

### Description / Requirements
Test `config.Load` and verify both `cmd/indexer/main.go` and `cmd/api/main.go` compile and wire correctly. This is the capstone of the happy path — after this issue, the system can be started with `go run cmd/indexer` and `go run cmd/api` against a real Postgres and RPC endpoint.

The API's `config.Load` currently requires indexer-specific env vars (`RPC_URL`, `FACTORY_ADDRESS`). Consider splitting into `LoadAPI` / `LoadIndexer`, or making those fields optional for the API process.

### Branch name
`indexer-api-happy-1-/entrypoints-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `cmd/indexer/main.go` — indexer entry point
- `cmd/api/main.go` — API entry point
- `internal/config/config.go` — `Config`, `Load()`
- `internal/log/level.go` — `ParseLevel()`

### Unit Tests (TDD)
- `config.Load` returns error when `DATABASE_URL` is missing
- `config.Load` returns error when `CHAIN_ID` is missing
- `config.Load` uses defaults for optional fields (`PORT=8080`, `LOG_LEVEL=info`, etc.)
- `config.Load` parses `POLL_INTERVAL` as a duration
- `config.Load` falls back `DatabaseReadURL` to `DatabaseURL` when unset
- `config.Load` parses all numeric fields correctly
- `ParseLevel` returns correct `slog.Level` for each string

### Integration Tests
- `cmd/api` starts, serves `/health`, and shuts down on SIGTERM
- `cmd/indexer` starts with mock RPC and shuts down on context cancel

### Dependencies
- Issues 1–6 (all happy-path components)

### Blocked by
Issues 1–6

### Principles
- 12-factor config: all config from environment variables.
- Migration ownership: both services run migrations on startup (considerations doc #2).

---

## 8. [DRAFT] Automated QA: happy-path verification

### Description / Requirements
This gate verifies the entire happy path works end-to-end before moving to resilience work. Should cover: indexer starts and indexes blocks into Postgres, API serves indexed data, all unit tests from issues 1–7 pass in CI.

### Branch name
`indexer-api-happy-1-/qa-happy-path-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- All files from issues 1–7

### Unit Tests (TDD)
- All unit tests from issues 1–7 pass
- CI pipeline runs all tests and reports green

### Integration Tests
- Indexer writes an AuctionCreated event to Postgres (real DB)
- API returns the indexed auction via GET /api/v1/auctions/{address} (real DB)

### Dependencies
- Issues 1–7

### Blocked by
Issue 7 (Config + entry points)

### Principles
None — verification gate.

---

# Phase 2 — Resilience

Add error handling, retries, and reorg support. The system works end-to-end from Phase 1; now make it survive real-world conditions.

---

## 9. Retry transport (exponential backoff + jitter)

### Description / Requirements
Implement `retryTransport.RoundTrip` and `newHTTPClientWithRetry`. Wire the retry transport into `eth.NewClient` (replacing the default transport from issue 6). This is the first layer of the two-layer retry defense (considerations doc #8).

### Branch name
`indexer-api-happy-1-/retry-transport-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/eth/transport.go` — `newHTTPClientWithRetry()`, `retryTransport.RoundTrip()`
- `internal/eth/client.go` — update `NewClient()` to use retry transport

### Unit Tests (TDD)
- `retryTransport` retries on 429 status up to `maxRetries` times
- `retryTransport` retries on 502, 503, 504 statuses
- `retryTransport` does not retry on 200, 400, 404 statuses
- `retryTransport` returns the last response after exhausting retries
- Backoff delay increases exponentially between attempts
- Jitter keeps delay within expected bounds (0.5x to 1.0x of calculated delay)
- `newHTTPClientWithRetry` returns an `http.Client` with the retry transport configured
- `isRetryableStatus` returns correct values for all tested codes

### Integration Tests
None — tested with a fake HTTP server.

### Dependencies
- Issue 6 (eth client)

### Blocked by
Issue 6 (Ethereum RPC client)

### Principles
- RPC retry strategy: exponential backoff + jitter (considerations doc #5).
- Two-layer defense: transport handles HTTP blips (considerations doc #8).

---

## 10. Indexer loop resilience (retry budget + error recovery)

### Description / Requirements
Test the indexer loop's error handling paths: `handleLoopError`, consecutive error counting, retry-then-exit behavior, and the sleep-between-retries pattern. These paths exist in the scaffold but were skipped in the happy-path tests.

### Branch name
`indexer-api-happy-1-/indexer-resilience-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/indexer/indexer.go` — `handleLoopError()`, retry paths in `Run()`

### Unit Tests (TDD)
- `handleLoopError` increments consecutive error counter
- `handleLoopError` returns false when under `maxLoopRetries`
- `handleLoopError` returns true when at `maxLoopRetries`
- Indexer retries on transient RPC error (`BlockNumber` fails then succeeds)
- Indexer retries on transient `FilterLogs` error
- Indexer retries on transient `HeaderByNumber` error
- Indexer retries on transient `WithTx` error (DB blip)
- Indexer resets consecutive error counter after a successful batch
- Indexer exits after `maxLoopRetries` consecutive errors
- Indexer sleeps `PollInterval` between retry attempts
- Indexer skips when chain is too young for confirmation buffer (head < confirmations)

### Integration Tests
None — all dependencies are mocked.

### Dependencies
- Issue 1 (mocks from indexer loop happy path)

### Blocked by
Issue 1 (Indexer loop happy path)

### Principles
- Indexer loop resilience: two-layer retry — transport handles HTTP blips, loop handles sustained outages (considerations doc #8).

---

## 11. Reorg detection and rollback

### Description / Requirements
Test `detectReorg` and `handleReorg`. These are the most complex pieces of the indexer — walking backwards to find a common ancestor and atomically rolling back all data past that point. All dependencies mocked.

### Branch name
`indexer-api-happy-1-/reorg-handling-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/indexer/reorg.go` — `detectReorg()`, `handleReorg()`
- `internal/indexer/indexer.go` — reorg path in `Run()` (step 2e/2f)

### Unit Tests (TDD)
- `detectReorg` returns false when stored hash matches chain hash
- `detectReorg` returns false when no stored hash exists (new block)
- `detectReorg` returns true when stored hash differs from chain hash
- `detectReorg` returns false at block 0 (genesis)
- `detectReorg` propagates block repo errors
- `detectReorg` propagates eth client errors
- `handleReorg` finds common ancestor by walking backwards
- `handleReorg` rolls back raw events, auctions, blocks, and cursor in one `WithTx`
- `handleReorg` returns the common ancestor block number
- `handleReorg` returns error when reorg exceeds `maxReorgDepth` (128)
- `handleReorg` gets ancestor hash from block repo for cursor update
- Indexer detects reorg at step 2e, calls `handleReorg`, resets cursor, and re-enters loop

### Integration Tests
None — all dependencies are mocked.

### Dependencies
- Issue 1 (mocks from indexer loop happy path)

### Blocked by
Issue 1 (Indexer loop happy path)

### Principles
- Atomic batches: rollback happens in a single transaction.
- Safety cap: `maxReorgDepth = 128` prevents unbounded rollback.

---

## 12. [DRAFT] Automated QA: resilience verification

### Description / Requirements
This gate verifies the system handles failures gracefully before moving to production readiness. Should cover: retry transport recovers from transient RPC errors, indexer loop survives sustained outages up to the retry budget, reorg rollback correctly restores data consistency.

### Branch name
`indexer-api-happy-1-/qa-resilience-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- All files from issues 9–11

### Unit Tests (TDD)
- All unit tests from issues 9–11 pass
- CI pipeline runs all tests and reports green

### Integration Tests
- Indexer recovers from a simulated RPC outage (transport retries succeed)
- Indexer exits cleanly after exceeding retry budget
- Reorg rollback leaves the database in a consistent state

### Dependencies
- Issues 9–11

### Blocked by
Issue 11 (Reorg detection and rollback)

### Principles
None — verification gate.

---

# Phase 3 — Production Readiness

Middleware, caching, health probes, and validation polish. The system is functional and resilient from Phases 1–2; now make it deployable.

---

## 13. Middleware: CORS, request ID, recovery, request logger

### Description / Requirements
Test the middleware chain in `internal/api/middleware.go`. All implementations are scaffold-complete — this issue is purely tests to verify correctness before deploying. Also test `Server` route registration and middleware ordering.

### Branch name
`indexer-api-happy-1-/middleware-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/api/middleware.go` — `cors`, `requestID`, `recovery`, `requestLogger`
- `internal/api/server.go` — middleware chain order

### Unit Tests (TDD)
- `cors` sets `Access-Control-Allow-Origin: *` on responses
- `cors` sets `Access-Control-Allow-Methods: GET, OPTIONS`
- `cors` returns 204 for OPTIONS preflight requests
- `cors` passes through non-OPTIONS requests to next handler
- `requestID` generates an ID when `X-Request-ID` header is absent
- `requestID` propagates client-provided `X-Request-ID`
- `requestID` sets `X-Request-ID` on the response
- `requestID` stores ID in context (retrievable via `RequestIDFromContext`)
- `requestID` stores request-scoped logger in context (retrievable via `LoggerFromContext`)
- `recovery` returns 500 JSON error when handler panics
- `recovery` passes through normally when handler does not panic
- `statusWriter.WriteHeader` captures the status code
- `requestLogger` logs method, path, status, and duration
- Full middleware chain (cors -> requestID -> recovery -> logger -> handler) produces correct headers

### Integration Tests
None.

### Dependencies
None beyond standard library.

### Blocked by
Nothing

### Principles
None specific — standard HTTP middleware concerns.

---

## 14. Health and readiness probes

### Description / Requirements
Test the `Health` handler and implement the `Ready` handler's DB connectivity check (currently a TODO). Add a `Ping(ctx) error` method to `store.Store` interface and implement it in `postgres.pgStore`. `Ready` should return 503 with `{"status":"not_ready","reason":"database unreachable"}` when ping fails.

### Branch name
`indexer-api-happy-1-/health-probes-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/api/handlers/health.go` — `Health()`, `Ready()` (TODO: ping DB)
- `internal/store/store.go` — `Store` interface (add `Ping`)
- `internal/store/postgres/postgres.go` — implement `Ping`

### Unit Tests (TDD)
- `Health` returns 200 with `{"status":"ok"}`
- `Health` sets `Cache-Control: no-store`
- `Ready` returns 200 with `{"status":"ready"}` when DB ping succeeds (mock store)
- `Ready` returns 503 with `{"status":"not_ready"}` when DB ping fails (mock store)
- `Ready` sets `Cache-Control: no-store`
- `Ping` on pgStore succeeds with a live database
- `Ping` on pgStore returns error with a closed pool

### Integration Tests
- `Ready` against a real database returns 200

### Dependencies
- Issue 4 (Postgres store — for `Ping` implementation)

### Blocked by
Issue 4 (Postgres store)

### Principles
- Health probes bypass middleware chain (considerations doc, server.go comments).

---

## 15. API caching headers

### Description / Requirements
Verify `Cache-Control` headers are set correctly per-endpoint per the caching strategy in considerations doc #1. The headers are already in the scaffold code — this issue adds focused tests and documents the caching contract.

### Branch name
`indexer-api-happy-1-/cache-headers-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- `internal/api/handlers/auction.go` — `Cache-Control: public, max-age=86400, immutable`
- `internal/api/handlers/health.go` — `Cache-Control: no-store`

### Unit Tests (TDD)
- `Get` auction sets `Cache-Control: public, max-age=86400, immutable` on 200
- `Get` auction does NOT set cache headers on 404
- `Get` auction does NOT set cache headers on 400
- `Get` auction does NOT set cache headers on 500
- `Health` sets `Cache-Control: no-store`
- `Ready` sets `Cache-Control: no-store`

### Integration Tests
None.

### Dependencies
- Issue 3 (API auction GET handler)

### Blocked by
Issue 3 (API auction GET)

### Principles
- Caching strategy: `Cache-Control` headers per-endpoint (considerations doc #1).
- Immutable data gets long TTL; health probes get `no-store`.

---

## 16. [DRAFT] Automated QA: production readiness verification

### Description / Requirements
This gate verifies the system is ready for deployment. Should cover: middleware chain applies correct headers and handles panics, health/readiness probes reflect real DB state, caching headers match the documented per-endpoint policy, full test suite passes in CI.

### Branch name
`indexer-api-happy-1-/qa-production-1`

### Scaffold
Branch: `indexer-api-happy-1-/scaffold-1`
- All files from issues 13–15

### Unit Tests (TDD)
- All unit tests from issues 13–15 pass
- CI pipeline runs full test suite (all phases) and reports green

### Integration Tests
- API request through full middleware chain returns correct CORS, X-Request-ID, and Cache-Control headers
- /ready returns 503 when DB is down, 200 when DB is up
- /health returns 200 regardless of DB state

### Dependencies
- Issues 13–15

### Blocked by
Issue 15 (API caching headers)

### Principles
None — verification gate.
