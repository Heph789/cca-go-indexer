# Implementation Milestones

## Phase 0: Project Skeleton

- Go module init
- Project directory structure
- CLAUDE.md with conventions
- Makefile with build/test/lint targets
- CI pipeline (GitHub Actions): lint, test, build
- Dockerfile (multi-stage)
- docker-compose for local dev (postgres)
- Linter config (golangci-lint)

## Phase 1: Domain + Database Foundation

- Domain types in `internal/domain/`
- PostgreSQL schema and migrations
- Store layer with CRUD for all entities
- Integration tests with testcontainers-go
- Indexer cursor management

## Phase 2: Ethereum Client + ABI Bindings

- ABI bindings for Factory and Auction contracts
- RPC client wrapper with retry and rate limiting
- Event log fetching and parsing
- Block hash tracking for reorg detection

## Phase 3: Indexer — Factory Events

- Indexer main loop (poll-based)
- Process `AuctionCreated` events
- Store new auctions in database
- Reorg detection and rollback
- Single chain (pick one testnet to start)
- Metrics and structured logging

## Phase 4: Indexer — Auction Events

- Process all auction events (bids, checkpoints, ticks, steps, exits, claims)
- Update auction derived state (clearing price, graduation)
- Full bid lifecycle tracking
- Backfill capability (process historical blocks efficiently)

## Phase 5: API Server

- HTTP server with routing (chi or stdlib mux)
- Auction endpoints (list, detail, state)
- Bid endpoints (by auction, by owner)
- Checkpoint and price history endpoints
- Pagination, filtering, error handling
- Health and readiness checks
- API metrics

## Phase 6: Multi-Chain

- Support multiple chains concurrently
- Per-chain configuration
- Chain-aware API responses
- Independent indexer progress per chain

## Phase 7: Polish + Production Hardening

- Load testing
- Query optimization (EXPLAIN ANALYZE on key queries)
- Connection pool tuning
- Rate limiting on API
- Docker Compose for full local stack
- Documentation for operators (deployment, monitoring, troubleshooting)

## Non-Goals (for now)

- WebSocket/SSE real-time streaming
- Write API (submitting bids)
- Token metadata enrichment (names, symbols, decimals from external sources)
- Frontend
- Authentication/authorization (public read-only API)
