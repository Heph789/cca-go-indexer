# Planning Summary

| | |
|---|---|
| **Purpose** | Initial architecture and design decisions before implementation. Living document — updated as decisions evolve. Not a source of truth. |
| **Created** | 2026-03-20 |
| **Updated** | 2026-03-20 |

## What We're Building

A Go indexer and REST API for Uniswap's Continuous Clearing Auction (CCA) contracts. The system discovers token sale auctions from the factory contract, indexes all on-chain events, stores them in PostgreSQL, and serves them via a read-only API.

## Domain

CCA is a time-weighted uniform clearing price auction for bootstrapping token liquidity. The factory (`0xCCccCcCAE7503Cac057829BF2811De42E16e0bD5`, v1.1.0) is deployed on Mainnet, Unichain, Base, Sepolia, and Unichain Sepolia.

**Entities:** Auctions, Bids (submitted → exited → claimed), Checkpoints (state snapshots per block, doubly-linked list), Ticks (price levels), Auction Steps (supply schedule segments).

**Numeric encoding:** Prices are Q96 fixed-point (scaled by 2^96). Internal accounting uses ValueX7 (scaled by 1e7).

**Events to index:**
- Factory: `AuctionCreated`
- Auction: `TokensReceived`, `BidSubmitted`, `CheckpointUpdated`, `ClearingPriceUpdated`, `BidExited`, `TokensClaimed`
- Tick: `TickInitialized`, `NextActiveTickUpdated`
- Step: `AuctionStepRecorded`
- Sweep: `TokensSwept`, `CurrencySwept`

## Architecture

```
cmd/indexer/    cmd/api/
       \           /
     internal/
       ├── domain/cca/  # CCA types (Auction, Bid, Checkpoint, Tick, Step)
       ├── indexer/     # poll loop, event processing, reorg handling
       ├── store/       # postgres (migrations, repositories)
       ├── api/         # HTTP handlers, routing
       ├── eth/         # RPC client, ABI bindings
       └── config/
```

- **Indexer**: per-chain polling loop, atomic writes per block, reorg rollback via block hash tracking
- **Store**: PostgreSQL via `pgx` (raw SQL), `chain_id` on every table, raw events table for replay, idempotent writes keyed by `(chain_id, block_number, tx_hash, log_index)`. Database tooling:
  - `pgx` as the driver — Postgres-native, fast, good `NUMERIC`/custom type support for uint256 values
  - Hand-written SQL to start. Indexer writes are event-driven (upserts, batch inserts, block-range rollbacks) which don't map well to ORMs. Read queries are specific enough that raw SQL is clearer.
  - `sqlc` is an option to add later if query count grows — it generates type-safe Go from SQL files, so you keep writing SQL but get compile-time safety. Low migration cost since the SQL stays the same.
  - `pgxpool` for connection pooling
- **API**: REST at `/api/v1/{chain_id}/...` — auctions, bids, checkpoints, price history, ticks. Cursor-based pagination. Health/readiness checks. Caching strategy:
  - In-memory cache (e.g. groupcache or simple TTL map) for hot paths: auction list, auction detail, clearing price
  - Finalized data is immutable and infinitely cacheable — ended auctions, exited bids, historical checkpoints
  - Active auction data gets short TTLs (seconds), keyed by latest indexed block so cache invalidates naturally on new blocks
  - HTTP cache headers (ETag/Last-Modified based on indexed block) to let downstream (CDN, reverse proxy) cache too
  - Start simple (in-process cache), add Redis only if needed for multi-instance API deployments
- **Eth client**: RPC wrapper with retry, rate limiting, ABI bindings

## Key Design Decisions

- Domain types are plain structs with no DB dependencies
- Single database, multi-chain (chain_id discriminator)
- Raw events stored alongside derived tables (audit trail + replay)
- Per-chain indexer goroutines with independent cursors. Tradeoffs vs. separate deployed processes:
  - **Goroutines (single process):** simpler deployment and config, shared DB pool, single binary to monitor. Downside: one chain's panic or memory leak takes down all chains, can't scale/restart chains independently.
  - **Separate processes:** independent failure domains, per-chain resource tuning and scaling, can restart one chain without affecting others. Downside: more operational overhead (multiple deployments, health checks, configs).
  - Starting with goroutines — simpler to build, easy to split later since each chain's indexer is already isolated by design.
- 12-factor config via environment variables

## Production Concerns

- Structured logging (slog), Prometheus metrics, liveness/readiness health checks. slog over zerolog because: stdlib (no dependency), good enough performance for this use case, and becoming the Go ecosystem default. zerolog is faster for extreme throughput but adds a dependency for marginal gain here. Easy to swap later since both produce structured JSON.
- Graceful shutdown with context propagation
- RPC resilience (retry, backoff, circuit breaker, fallback endpoints)
- Atomic block processing in transactions
- Integration tests with testcontainers-go

## LLM Development

- Small focused files, clear package DAG, consistent patterns
- Comprehensive CLAUDE.md as the LLM entry point
- Test coverage as feedback loop (LLM runs `go test ./...` to validate)
- Makefile for all common operations (build, test, lint, migrate, generate)
- Code generation for ABI bindings and SQL types

## Milestones

| Phase | Scope |
|---|---|
| 0 | Project skeleton, CI, Docker, Makefile, CLAUDE.md |
| 1 | Domain types, database schema, store layer, integration tests |
| 2 | Eth client, ABI bindings, event parsing |
| 3 | Indexer for factory events (single chain) |
| 4 | Indexer for all auction events, backfill |
| 5 | API server with all read endpoints |
| 6 | Multi-chain support |
| 7 | Production hardening, load testing, query optimization |
