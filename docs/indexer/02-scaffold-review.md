## Scaffold Review

### Overview

This is a well-structured scaffold for a blockchain event indexer targeting CCA (auction) contracts. The code establishes the full data flow: RPC polling → log filtering → event decoding → Postgres persistence, with cursor-based resumability and reorg detection. All implementation bodies are `panic("not implemented")` stubs with clear TODO comments — this is purely structural.

### What's Good

- **Clean separation of concerns.** The layering is solid: `eth.Client` interface abstracts RPC, `store.Store` interface abstracts persistence, `EventHandler` interface abstracts event-specific logic, and the indexer loop ties them together. Each layer is independently testable via interfaces.

- **Transactional atomicity.** The `WithTx` pattern ensuring events + block hashes + cursor advance all commit together is correct and critical. No partial blocks can be written.

- **Reorg handling design.** Walking backwards to a common ancestor with a `maxReorgDepth` safety cap, then atomic rollback of all data after the ancestor, is a sound approach. The 128-block cap is reasonable.

- **Handler registry.** The `EventHandler` interface + `HandlerRegistry` is a clean plugin pattern. Adding a new event type is truly just a new handler file + one line in `main.go`.

- **Raw event audit trail.** Dual-writing both raw logs and typed records is good — enables replay and schema evolution.

### Design Concerns

1. **Block header fetching is N+1.** In `indexer.go:133-143`, you fetch one header per block in the batch range sequentially. For a batch of 100 blocks, that's 100 RPC calls. Consider:
   - Batching via `eth_getBlockByNumber` JSON-RPC batch requests
   - Or fetching only the last block's header for cursor hash, and using `eth_getLogs` result block hashes for the rest (each `types.Log` already has `BlockHash`)
<-- this is an implementation detail. We should consider this then, but no need to change scaffold for now.

2. **`safeHead` underflow.** At `indexer.go:80`, if `chainHead < config.Confirmations`, this underflows to a huge uint64. Add a guard:
   ```go
   if chainHead < idx.config.Confirmations {
       // chain too young, sleep
   }
   ```
<-- implementation detail

3. **`querier` interface is empty.** In `postgres.go:17-21`, the placeholder comment says "both pgxpool.Pool and pgx.Tx satisfy this" but the interface has no methods. When you implement this, you'll want the common subset (`Query`, `QueryRow`, `Exec` from pgx). Consider using `pgx.Tx` directly or defining the exact method set now so the compiler catches mismatches.

4. **`WatchedContractRepository` is defined but unused.** It's in `store.go:74-79` and has a migration table, but `Store` doesn't expose it and the indexer doesn't use it. The plan mentions dynamic address watching (factory creates auction → watch auction address). If that's MVP scope, wire it through `Store`; if not, remove it from the migration to avoid orphaned tables.
<-- we should clean this up

5. **No retry/backoff in the indexer loop.** If `FilterLogs` or `BlockByNumber` returns a transient RPC error, the indexer halts immediately (`return fmt.Errorf(...)`). The `eth.Client` TODOs mention retry, but even with client-level retry, the indexer loop itself should probably distinguish transient vs fatal errors and retry with backoff rather than exiting.
<-- how would retry work on the transport layer vs the indexer loop?

6. **`MaxBlockRange` config is unused.** It's defined in config but never referenced. `BlockBatchSize` is what controls the range. Clarify if these are meant to be different things (batch size vs RPC limit) or consolidate.
<-- imp detail

7. **`domain/chain.go` ChainID constants.** These are defined but never used. The indexer takes `ChainID` from config as a plain `int64`. Either use `domain.ChainID` type throughout or remove the constants if they're not needed yet.
<-- imp detail

8. **`factory.json` is empty / placeholder.** The `//go:embed factory.json` will panic at init if the JSON doesn't contain a valid ABI with an `AuctionCreated` event. Make sure a valid ABI stub is in place before this compiles.

### ABI Bindings

The scaffold manually parses the factory ABI JSON and unpacks events by hand (`abi.JSON` + `abi.Unpack`). This works for MVP with one event, but becomes repetitive as more events are added.

`abigen` (from go-ethereum) can generate typed Go bindings from an ABI — structs for each event and type-safe parse methods. It generates more than we need (contract callers, transactors, filterers), but the indexer only needs the event structs and parsing. Consider adopting it when adding the second or third event type.

### Minor Notes

- The `Dockerfile` copies `go.sum` but it doesn't exist yet (no `go mod tidy` has been run). Will fail on build.
- Migration down script drops tables in correct dependency order — good.
- `auctions` PK is `(chain_id, auction_address)` — this assumes one auction per address per chain, which makes sense for a factory pattern.

### Summary

The architecture is clean and the approach is sound for an MVP indexer. The main things to address before implementation are the N+1 block header problem (performance), the uint64 underflow on `safeHead` (correctness), and deciding whether `WatchedContractRepository` / `MaxBlockRange` are in scope or should be pruned.
