# Architecture

## High-Level Design

```
                    ┌─────────────┐
                    │  RPC Nodes  │
                    │ (per chain) │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   Indexer   │
                    │  (per chain)│
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  PostgreSQL │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   API Server│
                    └─────────────┘
```

The indexer and API are separate concerns but can run in the same binary (toggled by config/flags) or as separate processes.

## Project Structure

```
cmd/
  indexer/main.go              # indexer entrypoint
  api/main.go                  # api entrypoint
internal/
  domain/                      # core types — Auction, Bid, Checkpoint, etc.
    auction.go
    bid.go
    checkpoint.go
    tick.go
    step.go
  indexer/                     # chain reading, event processing
    indexer.go                 # main indexer loop
    processor.go               # event → domain mapping
    reorg.go                   # reorg detection and rollback
  store/                       # database layer
    postgres/
      migrations/              # SQL migration files
      auction.go
      bid.go
      checkpoint.go
      store.go                 # connection management, transactions
  api/                         # HTTP handlers and routing
    server.go
    handlers/
      auction.go
      bid.go
  eth/                         # Ethereum client wrappers
    client.go                  # RPC client with retry/rate-limit
    abi/                       # generated ABI bindings
      factory.go
      auction.go
  config/                      # configuration loading
    config.go
```

## Key Design Decisions

### Separate domain from storage

Domain types in `internal/domain/` are plain Go structs with no database tags or dependencies. Store implementations translate between domain types and database rows. This keeps the domain clean and testable.

### Repository pattern for storage

Each entity gets a repository interface defined alongside its domain type. The postgres package implements these interfaces. This makes it possible to test business logic without a database when appropriate, while still defaulting to integration tests with a real database.

### Per-chain indexer instances

Each supported chain runs its own indexer goroutine with its own RPC client and cursor (last processed block). This keeps chain-specific concerns isolated and allows independent progress/backfill per chain.

### Single database, multi-chain

All chains write to the same PostgreSQL database, with `chain_id` as a discriminator on every table. This simplifies the API layer — queries can span chains or filter by chain.

### Event sourcing lite

Raw events are stored in an `events` table alongside the derived domain tables. This provides:
- An audit trail
- The ability to rebuild domain state by replaying events
- Debugging capability when derived state looks wrong

### Idempotent processing

Every event is keyed by `(chain_id, block_number, tx_hash, log_index)`. Reprocessing the same block is safe — upsert semantics prevent duplicates.

## Reorg Handling

Chain reorgs are a first-class concern:

1. Track the last N block hashes (configurable finality window per chain)
2. On each poll cycle, verify that the parent hash of the new block matches our stored hash
3. If mismatch detected: roll back to the common ancestor by deleting events and derived state for orphaned blocks
4. Re-index from the common ancestor forward

For the API, consider a `confirmed` flag or a configurable finality depth for queries.

## Configuration

Environment-variable driven (12-factor), with a config struct:

```
DATABASE_URL           # postgres connection string
CHAINS                 # comma-separated chain configs
RPC_URL_{CHAIN_ID}     # RPC endpoint per chain
START_BLOCK_{CHAIN_ID} # optional override for starting block
LOG_LEVEL              # debug, info, warn, error
API_PORT               # HTTP listen port
METRICS_PORT           # Prometheus metrics port
```
