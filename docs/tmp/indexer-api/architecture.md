# CCA Indexer — Architecture

## High-Level Data Flow

```
Blockchain (RPC)
       |
       v
+----------------------------------------------+
|              Indexer Service                  |
|  cmd/indexer/main.go                         |
|                                              |
|  +-----------+    +----------------------+   |
|  |  Config   |--->|   ChainIndexer       |   |
|  +-----------+    |   (polling loop)     |   |
|                   |                      |   |
|                   |  1. Load cursor      |   |
|                   |  2. Poll chain head  |   |
|                   |  3. Detect reorgs    |   |
|                   |  4. Fetch logs       |   |
|                   |  5. Dispatch events  |   |
|                   |  6. Advance cursor   |   |
|                   +----------+-----------+   |
|                              |               |
|                   +----------v-----------+   |
|                   |  Handler Registry    |   |
|                   |  topic0 -> Handler   |   |
|                   +----------+-----------+   |
|                              |               |
|                   +----------v-----------+   |
|                   | AuctionCreated       |   |
|                   | Handler              |   |
|                   | (decode + persist)   |   |
|                   +----------------------+   |
+----------------------|------------------------+
                       |
                       v
+----------------------------------------------+
|              PostgreSQL                       |
|                                              |
|  indexer_cursors    — per-chain progress      |
|  raw_events         — audit trail            |
|  event_ccaf_auction_created — decoded events       |
|  indexed_blocks     — reorg detection        |
|  watched_contracts  — dynamic address list   |
+----------------------------------------------+
                       ^
                       |
+----------------------------------------------+
|              API Service                     |
|  cmd/api/main.go                             |
|                                              |
|  +-----------+    +----------------------+   |
|  |  Config   |--->|   HTTP Server        |   |
|  +-----------+    |   (net/http mux)     |   |
|                   +----------+-----------+   |
|                              |               |
|                   +----------v-----------+   |
|                   |     Middleware        |   |
|                   |  CORS > ReqID >      |   |
|                   |  Recovery > Logger   |   |
|                   +----------+-----------+   |
|                              |               |
|                   +----------v-----------+   |
|                   |      Handlers        |   |
|                   |  /health             |   |
|                   |  /ready              |   |
|                   |  /api/v1/auctions/   |   |
|                   +----------------------+   |
+----------------------------------------------+
       |
       v
    Client
```

## Package Structure

```
cmd/
  indexer/main.go          Entry point: wires config, RPC, store, indexer
  api/main.go              Entry point: wires config, store, HTTP server

internal/
  config/                  Env-based config (12-factor)
  domain/
    cca/                   Pure data types: Auction, RawEvent
    chain.go               ChainID type alias
  eth/
    client.go              RPC client interface (wraps go-ethereum)
    transport.go           HTTP retry transport (exp backoff)
    abi/factory.go         Embedded ABI, topic0 extraction
  indexer/
    indexer.go             Polling loop, batch processing
    handler.go             EventHandler interface + registry
    reorg.go               Reorg detection & rollback
    handlers/
      auction_created.go   Decode AuctionCreated log -> domain type
  store/
    store.go               Repository interfaces (contract)
    postgres/
      postgres.go          pgxpool setup, WithTx, migrations
      auction.go           AuctionRepository (insert, get, delete)
      cursor.go            CursorRepository (get, upsert)
      block.go             BlockRepository (insert, get hash, delete)
      raw_event.go         RawEventRepository (insert, delete)
    migrations/
      000001_init.up.sql   Schema DDL
  api/
    server.go              HTTP server, route registration
    middleware.go           CORS, request ID, recovery, logging
    httputil/response.go   JSON envelope helpers
    handlers/
      auction.go           GET /api/v1/auctions/{address}
      health.go            GET /health, GET /ready
```

## Key Interfaces

```
eth.Client
  BlockNumber(ctx) -> uint64
  HeaderByNumber(ctx, *big.Int) -> *types.Header
  FilterLogs(ctx, FilterQuery) -> []types.Log
  Close()

store.Store
  AuctionRepo()   -> AuctionRepository
  RawEventRepo()  -> RawEventRepository
  CursorRepo()    -> CursorRepository
  BlockRepo()     -> BlockRepository
  WithTx(ctx, fn) -> error       // atomic transaction wrapper

indexer.EventHandler
  EventName() -> string
  EventID()   -> common.Hash     // topic0
  Handle(ctx, chainID, log, store) -> error
```

## Indexer Loop Detail

```
              start
                |
                v
        load cursor from DB
        (or use StartBlock)
                |
                v
  +---> get chain head via RPC
  |             |
  |             v
  |     safe_head = head - confirmations
  |             |
  |       at head? ---yes---> sleep(PollInterval) --+
  |             |                                    |
  |            no                                    |
  |             v                                    |
  |     batch = [cursor+1 .. safe_head]              |
  |             |                                    |
  |             v                                    |
  |     detect reorg?                                |
  |        |        |                                |
  |       yes       no                               |
  |        |        |                                |
  |        v        v                                |
  |     rollback   fetch logs (eth_getLogs)           |
  |     to common  fetch block headers               |
  |     ancestor        |                            |
  |        |            v                            |
  |        |     BEGIN TX                            |
  |        |       dispatch logs -> handlers         |
  |        |       insert block hashes               |
  |        |       upsert cursor                     |
  |        |     COMMIT                              |
  |        |            |                            |
  +--------+------------+<---------------------------+
```

## Reorg Handling

```
  stored hash for block N != chain hash for block N
                    |
                    v
          walk backwards from N
          until stored hash == chain hash
          (find common ancestor)
                    |
                    v
          safety check: depth <= 128
                    |
                    v
          BEGIN TX
            delete events   where block >= ancestor
            delete blocks   where block >= ancestor
            reset cursor    to ancestor
          COMMIT
```

## Design Principles

- **Atomic batches**: all events + block hashes + cursor in one DB transaction
- **Idempotent writes**: `ON CONFLICT DO NOTHING` — safe to re-index blocks
- **Raw + typed storage**: raw_events for audit/replay, typed tables for queries
- **Interface-driven**: eth.Client, store.Store, EventHandler all mockable
- **Resumable**: cursor in DB, restart picks up from last committed position
- **Multi-chain ready**: all tables keyed by chain_id
