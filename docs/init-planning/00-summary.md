# Planning Summary

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
       ├── domain/     # pure types (Auction, Bid, Checkpoint, Tick, Step)
       ├── indexer/     # poll loop, event processing, reorg handling
       ├── store/       # postgres (migrations, repositories)
       ├── api/         # HTTP handlers, routing
       ├── eth/         # RPC client, ABI bindings
       └── config/
```

- **Indexer**: per-chain polling loop, atomic writes per block, reorg rollback via block hash tracking
- **Store**: PostgreSQL, `chain_id` on every table, raw events table for replay, idempotent writes keyed by `(chain_id, block_number, tx_hash, log_index)`
- **API**: REST at `/api/v1/{chain_id}/...` — auctions, bids, checkpoints, price history, ticks. Cursor-based pagination. Health/readiness checks.
- **Eth client**: RPC wrapper with retry, rate limiting, ABI bindings

## Key Design Decisions

- Domain types are plain structs with no DB dependencies
- Single database, multi-chain (chain_id discriminator)
- Raw events stored alongside derived tables (audit trail + replay)
- Per-chain indexer goroutines with independent cursors
- 12-factor config via environment variables

## Production Concerns

- Structured logging (slog), Prometheus metrics, liveness/readiness health checks
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
