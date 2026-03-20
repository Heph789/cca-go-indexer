# Planning Summary

## What We're Building

A Go indexer and REST API for Uniswap's Continuous Clearing Auction (CCA) contracts. The system discovers token sale auctions from the factory contract, indexes all on-chain events (bids, clearing prices, checkpoints, exits, claims), stores them in PostgreSQL, and serves them via a read-only API.

## Domain

CCA is a time-weighted uniform clearing price auction for bootstrapping token liquidity. The factory (`0xCCcc...0bD5`) is deployed on Mainnet, Unichain, Base, and Sepolia. Each auction has a lifecycle: deploy → fund → bid → end → exit → claim → sweep. The indexer tracks this full lifecycle across chains.

Key entities: **Auctions**, **Bids** (with a submitted → exited → claimed lifecycle), **Checkpoints** (state snapshots per block), **Ticks** (price levels), and **Auction Steps** (supply schedule). Prices use Q96 fixed-point encoding.

## Architecture

- **Indexer**: per-chain polling loop that fetches events, processes them into domain types, and writes to the database atomically per block. Handles reorgs by tracking block hashes and rolling back to common ancestors.
- **Store**: PostgreSQL with a raw events table (audit/replay) plus derived domain tables. Multi-chain via `chain_id` on every table. Idempotent writes keyed by `(chain_id, block_number, tx_hash, log_index)`.
- **API**: REST with chain ID in the path (`/api/v1/{chain_id}/...`). Endpoints for auctions, bids, checkpoints, price history. Cursor-based pagination. Health/readiness checks.
- **Eth client**: RPC wrapper with retry, rate limiting, and ABI bindings for event parsing.

## Production Readiness

Structured logging (slog), Prometheus metrics (indexer lag, RPC errors, API latency), graceful shutdown via context cancellation, connection pooling, atomic block processing, and 12-factor configuration via environment variables. Integration tests with testcontainers-go.

## LLM Development

The codebase is structured to maximize LLM effectiveness: small focused files, clear package dependency DAG, consistent patterns, a comprehensive CLAUDE.md, and strong test coverage as a feedback loop. Code generation for ABI bindings and SQL types. Makefile for all common operations.

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

Detailed planning docs: [domain model](01-domain-model.md), [events](02-events.md), [architecture](03-architecture.md), [database schema](04-database-schema.md), [API design](05-api-design.md), [production readiness](06-production-readiness.md), [LLM development](07-llm-development.md), [milestones](08-milestones.md).
