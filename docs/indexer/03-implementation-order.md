# Implementation Order

Consumer-first, TDD. Start from the indexer loop and work outward — let tests reveal which dependencies are actually needed rather than speculatively building repos and helpers.

## Phases

### - [ ] Phase 1: Indexer loop + reorg

The core of the system. All dependencies (eth client, store) are mocked at the interface level. Tests drive out exactly which interface methods are needed and how they're called.

1. **Indexer loop** (`internal/indexer/indexer.go`)
   - Happy path: fetch logs, process, advance cursor
   - Sleep at head, catch up when behind
   - Context cancellation / graceful shutdown
   - safeHead underflow guard

2. **Reorg detection + rollback** (`internal/indexer/reorg.go`)
   - Hash mismatch detection
   - Walk-back to common ancestor
   - Atomic rollback
   - Max depth safety cap

These two can be developed together since reorg is called from the loop.

### - [ ] Phase 2: Event handling

Still mocking the store. Can be done in parallel with phase 1 since the `EventHandler` interface is small and stable.

1. **Handler registry** (`internal/indexer/handler.go`)
   - Topic filter construction
   - Dispatch by topic0
   - Unknown topic handling

2. **AuctionCreated handler** (`internal/indexer/handlers/auction_created.go`)
   - Decode a `types.Log` into domain types
   - Requires real ABI — obtain before starting this

### - [ ] Phase 3: Store implementation

By now, phases 1–2 have defined exactly which repo methods are called. Implement only those. Uses testcontainers for a real Postgres instance.

1. **Migrations** — schema from scaffold, trimmed to what's actually needed
2. **Repo implementations** — only the methods tests in phases 1–2 actually called
3. **WithTx** — transaction semantics, rollback on error

### - [ ] Phase 4: Wiring

1. **Config** (`internal/config/config.go`)
2. **Eth client** (`internal/eth/`) — thin interface + retry transport
3. **`cmd/indexer/main.go`** — connect everything, signal handling
4. Manual run against Sepolia

## Notes

- Interfaces will evolve during phases 1–2. That's the point — the consumer defines the contract.
- The scaffold on `indexer-1-/scaffold-1` is a reference, not a starting point. Copy patterns, not code.
- Domain types (`internal/domain/`) are just structs with no logic — add them as needed, no dedicated phase.
