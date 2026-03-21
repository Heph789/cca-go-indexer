# Indexer Overview

The indexer is a long-running process that watches CCA contracts on-chain, decodes their events, and writes structured data to PostgreSQL. It starts with a single event on a single chain but is designed so adding events and chains requires no changes to the core loop.

## How It Works

```
RPC Node → poll for new blocks → fetch logs → decode events → write to postgres
                ↑                                                    |
                └────────── advance cursor ──────────────────────────┘
```

1. **Poll** — ask the RPC node for the current block height
2. **Batch** — fetch logs from `cursor+1` to `min(cursor+batchSize, head)` in one `eth_getLogs` call
3. **Decode** — match each log's topic to a registered handler, decode it into a domain type
4. **Write** — in a single DB transaction: insert raw event, insert typed event record, record block hash, advance cursor
5. **Repeat** — if still behind head, immediately process next batch; otherwise sleep and poll again

The cursor lives in the database, so the process is fully resumable after restarts.

## Reorg Safety

Every indexed block's hash is stored. Before processing new blocks, the indexer checks that the chain's hash for the previous block matches what's stored. On mismatch:

1. Walk backwards to find the last block where hashes agree (common ancestor)
2. Delete all data after that block in one transaction
3. Resume indexing from there

Max rollback depth is capped (128 blocks) as a safety valve.

## Extensibility

### Adding an event

Each event type is an `EventHandler` — a small interface that knows its topic hash, how to decode a log, and which repository to write to. Adding an event means:

1. Define the domain type
2. Add a DB table (migration)
3. Add a repository
4. Implement the handler
5. Register it at startup

The indexer loop, reorg handling, and transaction management don't change. The handler registry automatically updates the `eth_getLogs` topic filter.

### Adding a chain

Each chain gets its own `ChainIndexer` goroutine with an independent cursor. Same database — `chain_id` on every table keeps data separated. Adding a chain is config, not code.

### Adding contracts to watch

Watched addresses live in a `watched_contracts` table in the database. On startup and periodically during the poll loop, the indexer loads the current set of addresses to include in `eth_getLogs` calls. The factory address is seeded into this table on first run.

New addresses can be added in two ways:
- **Statically** — insert directly into the table (config, migration, or admin tooling)
- **Dynamically** — an event handler inserts a new address as part of processing (e.g. a factory event emits a child contract address, and the handler adds it to `watched_contracts` within the same transaction)

The handler registry dispatches by topic, not by source contract, so handlers work regardless of which address emitted the event.

## Data Model

Each event type gets its own table (e.g. `auction_created_events`, `bid_submitted_events`) storing the decoded event fields alongside block context (chain_id, block_number, tx_hash, log_index). This keeps queries simple and typed — no need to decode JSON blobs at read time.

A general `raw_events` table also stores every log as-is (topics, data, block hash) for auditing and replay. If an event type's schema changes, the raw data is always there to re-derive from.

Most read queries should work directly against the per-event tables. Derived/aggregate tables (e.g. a materialized auction state) are only added when there's a concrete query pattern that can't be served efficiently from event tables alone.

Supporting tables:
- `watched_contracts` — addresses the indexer fetches logs for, per chain
- `indexer_cursors` — per-chain progress tracker
- `indexed_blocks` — per-block hash storage for reorg detection

## Atomicity

All writes for a block range happen inside a single postgres transaction (`Store.WithTx`). If any step fails — decoding, inserting a raw event, inserting a typed event record, updating the cursor — nothing is committed. This means the database is always in a consistent state: no partial blocks, no cursor ahead of data.

## Key Dependencies

- `go-ethereum` — ABI decoding, RPC client, Ethereum types
- `pgx` — postgres driver with connection pooling
- `golang-migrate` — schema migrations (embedded in binary)
