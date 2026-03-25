# API Considerations

Each section covers a design consideration: the problem, options with trade-offs, and the decision (or lack thereof).

---

## 1. Caching Strategy

**Problem:** Read-heavy endpoints serving immutable data (e.g. indexed events) could see burst traffic (10k+ concurrent requests for the same auction). Without caching, every request hits the database.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. HTTP Cache-Control headers + CDN** | Zero application complexity; CDN absorbs bursts; per-endpoint control via headers; industry standard | Requires CDN infra; stale data possible during reorgs |
| **B. In-process LRU cache** | No external infra; low latency | Memory pressure; stale data on reorgs; doesn't help across multiple API instances |
| **C. Redis/external cache** | Shared across instances; explicit invalidation | Operational overhead; another dependency; still need cache invalidation logic |
| **D. No caching** | Simplest | DB is the bottleneck under burst traffic |

**Decision: Option A — HTTP Cache-Control headers**

Set `Cache-Control` per-endpoint in the handler. A CDN at the infrastructure layer respects these headers with no application-level cache code.

Per-endpoint policies:

| Endpoint | Data nature | Header | Rationale |
|---|---|---|---|
| `GET /api/v1/auctions/{address}` | Immutable once indexed | `public, max-age=86400, immutable` | Event data never changes after indexing |
| Future: mutable/live data | Changes with new blocks | `no-store` or `public, max-age=5` | Short TTL or no cache depending on freshness needs |
| `GET /health`, `GET /ready` | Real-time status | `no-store` | Must always reflect current state |

Implementation — set headers in each handler rather than a global default:

```go
w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
```

**Reorg risk:** Low. Confirmed blocks (behind the `Confirmations` buffer) are unlikely to reorg, and `max-age=86400` means any stale entry expires within a day. CDN cache purge APIs can be called during rollback if needed.

---

## 2. Migration Ownership

**Problem:** Both the API and indexer services run `migrate.Up()` on startup. If both start simultaneously with pending migrations, who owns the migration?

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Both services run migrations on startup** | Simple; no separate deploy step; services are self-contained | Concurrent startup relies on advisory locks; harder to debug migration failures; couples schema changes to service deploys |
| **B. Dedicated migrate step in deploy pipeline** | Explicit control over when schema changes happen; decoupled from service startup; easier rollback | Extra deploy step; another thing to maintain; overkill for small teams |
| **C. Only one service runs migrations** | Avoids concurrency question | Creates a startup ordering dependency between services |

**Decision: Option A for now — both services run migrations on startup**

`golang-migrate` uses Postgres advisory locks (`pg_advisory_lock`) to serialize concurrent runs. If both services start at the same time, one acquires the lock and applies migrations while the other blocks. When the second service gets the lock, it sees everything is already applied and proceeds. This is safe out of the box.

Revisit with Option B (dedicated migrate step) when:
- The team grows and migrations need review gates
- Deploys become more complex (blue/green, canary)
- Migration failures need to be surfaced independently of service health

---

## 3. Foreign Key Between Events and Watched Contracts

**Problem:** `raw_events.address` and `event_ccaf_auction_created.auction_address` logically correspond to entries in `watched_contracts`, but there's no FK enforcing this relationship.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Add FK now** | Enforces referential integrity; makes relationship explicit | Adds insert ordering constraints inside the indexer transaction (must insert into `watched_contracts` before the event); factory address must be seeded in `watched_contracts` |
| **B. Add FK later** | No ordering constraints; simpler transaction logic | Relationship is implicit; possible to insert orphan events (in theory) |
| **C. No FK, document the relationship** | Simplest; no runtime overhead | No enforcement; relies on application logic |

**Decision: Option B — skip FK for now, add later if needed**

The indexer only fetches logs for watched addresses, so data consistency is guaranteed by application logic. Adding the FK later is a single `ALTER TABLE` migration with no data changes required. Revisit if data integrity becomes a concern or if new writers are introduced.

---

## 4. Rate Limiting

**Problem:** The API is public with no authentication. Without rate limiting, a single actor could overwhelm the server with requests.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Reverse proxy rate limiting (Cloudflare, nginx)** | Zero application code; handled at the edge before requests hit origin; same layer as CDN caching; DDoS protection included (Cloudflare) | Requires proxy infra; per-IP only unless combined with API keys |
| **B. Application-level middleware (`x/time/rate`)** | No external deps; fine-grained control | Only works for a single instance; behind a load balancer needs shared state (Redis) |
| **C. API keys + per-key limits** | Per-consumer visibility; easy revocation; ties abuse to identity | More overhead; need key management; overkill for a public read-only API |
| **D. Redis-backed rate limiting** | Shared across instances; per-IP or per-key | Another dependency; operational overhead |

**Decision: Option A — reverse proxy rate limiting**

Per-IP rate limiting at the proxy layer (Cloudflare, nginx, Caddy) stops casual abuse with no application code. This is the same layer that handles caching from consideration #1.

**Limitations of per-IP rate limiting:** IP spoofing is impractical for HTTP (TCP handshake prevents it), but a determined attacker with access to botnets or rotating cloud IPs can bypass per-IP limits. For a public read-only API serving on-chain data (which is already public), this is an acceptable trade-off — the attack incentive is low and the data isn't sensitive.

**If abuse escalates**, layer on:
1. API keys (ties usage to an identity)
2. Cloudflare Bot Management (fingerprinting, behavior analysis)
3. WAF rules (block suspicious request patterns)

---

## 5. RPC Retry Strategy

**Problem:** The Ethereum RPC provider can return transient errors (rate limiting 429, upstream failures 502/503/504). Without retries, a single blip causes the indexer to fail a batch and wait for the next poll interval. Without jitter on the backoff, multiple instances (or retry attempts) can synchronize their retries, causing a thundering herd that makes rate limiting worse.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Exponential backoff only** | Simple; reduces retry frequency over time | All instances retry at the same time (thundering herd) |
| **B. Exponential backoff + jitter** | Spreads retries over time; avoids synchronized bursts | Slightly more complex (one extra line: multiply by random factor) |
| **C. Fixed delay retries** | Simplest | Doesn't adapt to sustained outages; still herds |

**Decision: Option B — exponential backoff with jitter**

The `retryTransport` in `internal/eth/transport.go` retries at the HTTP layer, transparent to `ethclient`. Delay formula:

```
delay = baseDelay * 2^attempt * random(0.5, 1.0)
```

This is a two-layer defense:
1. **Transport retries** handle transient blips (429, 5xx) with backoff + jitter
2. **Polling loop** retries the entire batch on the next interval if transport retries are exhausted (cursor only advances on success, so no data is lost)

---

## 6. Connection Pool Tuning

**Problem:** pgxpool defaults to `max_conns = max(4, numCPU)`. Under thousands of concurrent API readers, requests block waiting for a free connection, causing cascading latency spikes. The indexer's write transactions further contend with API reads if they share the same pool.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Connection string params** (`pool_max_conns`, `pool_min_conns`, `pool_max_conn_idle_time` as query params on `DATABASE_URL`) | Operator-controlled without code changes; pgxpool supports natively; 12-factor friendly | Less discoverable than explicit config fields; pool settings are buried in the URL |
| **B. Explicit config fields** (`DB_MAX_CONNS`, `DB_MIN_CONNS`, `DB_MAX_CONN_IDLE_TIME` env vars parsed in `config.Load`) | Discoverable; self-documenting; validated at startup | More config surface; duplicates what pgxpool already parses from the URL |
| **C. Defaults only** | Simplest | Bottleneck under load; no operator control without code changes |

**Decision: Open**

Leaning toward Option A — pgxpool already parses pool params from the connection string, so this works with zero code changes:

```
postgres://user:pass@host/db?pool_max_conns=20&pool_min_conns=4&pool_max_conn_idle_time=5m
```

Right-sizing depends on deployment topology. Guidelines:
- **Single API pod, small RDS:** 10–20 max conns
- **Multiple pods behind LB:** 5–10 per pod to stay under Postgres `max_connections` (default 100)
- **Indexer:** needs far fewer connections (1–2 for the polling loop), but should have its own pool or at least its own `DATABASE_URL` to avoid contention with API reads
- **Separate read replica:** if the API points at a read replica, pool sizing for API and indexer become independent concerns

---

## 7. Read Replica for API

**Problem:** The API and indexer share `DATABASE_URL`. The indexer holds write transactions (events + blocks + cursor per batch), and the API runs concurrent reads against the same Postgres instance. Under load, write locks and connection contention degrade API read latency.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Separate `DATABASE_READ_URL` env var** | API points at a read replica; zero contention with indexer writes; falls back to primary if unset; no interface changes | Replication lag (typically sub-second); requires replica infra |
| **B. Application-level read/write splitting** | Single connection string; automatic routing | Complex; needs proxy (PgBouncer, ProxySQL) or driver support; harder to reason about |
| **C. Shared URL** | Simplest | Write contention degrades API reads under load |

**Decision: Option A — separate `DATABASE_READ_URL`**

Added `DatabaseReadURL` to `Config`. Falls back to `DatabaseURL` if unset, so single-instance deployments work unchanged. The API's `cmd/api/main.go` now connects via `cfg.DatabaseReadURL`.

Replication lag is acceptable because indexed data is already `Confirmations` blocks behind chain head — sub-second replica lag is negligible compared to the confirmation buffer.

---

## 8. Indexer Loop Resilience

**Problem:** The indexer loop exited on any RPC or DB error with no retry. A single transient 503 or momentary DB connection blip killed the process, relying entirely on external supervision (k8s/systemd) to restart. Restarts are expensive — reconnecting, re-loading cursor, and missing time during the restart window.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Transport retries only** | Single retry layer; simple | Doesn't cover DB errors; bounded budget means sustained outages still crash |
| **B. Transport retries + bounded loop retries** | Two-layer defense; transport handles HTTP blips, loop handles longer outages and DB errors; still crashes after budget is exhausted so supervision gets alerted | More complex error flow; need to track consecutive errors |
| **C. Unbounded loop retries** | Never crashes; maximum availability | Silent degradation; no crash alert if something is fundamentally broken |

**Decision: Option B — two-layer retry**

**Transport layer** (handles individual RPC call flakiness):
- 5 retries, 500ms base delay, exponential backoff + jitter
- Worst-case budget per call: ~24s
- Covers: 429 rate limits, 502/503/504 transient errors

**Loop layer** (handles sustained outages and DB errors):
- 5 consecutive error tolerance before exit
- Sleeps `PollInterval` (default 12s) between retries
- Resets on any successful batch
- Covers: RPC down for minutes, DB connection loss, any error the transport can't absorb

Combined worst case: transport exhausts ~24s per call, loop retries 5 times with 12s sleep between = ~3 minutes of tolerance before crash. This gives operators time to notice and fix issues while still crashing on genuinely broken state.

Also fixed a uint64 underflow bug: `chainHead - Confirmations` wraps to a huge number when the chain is younger than the confirmation buffer. Added a guard that sleeps instead.

---

## 9. Parallel Block Header Fetches

**Problem:** The indexer fetches block headers sequentially in a loop (`indexer.go`). With a batch size of 100 and ~50ms per RPC call, header fetching alone takes ~5 seconds per batch. During catch-up (thousands of blocks behind), this dominates indexing time.

**Options:**

| Option | Pros | Cons |
|---|---|---|
| **A. Bounded `errgroup` concurrency** | Simple; works with any RPC provider; uses existing `eth.Client` interface unchanged; standard Go pattern | Still N HTTP requests (just concurrent); need to size concurrency limit to avoid overwhelming the RPC provider |
| **B. JSON-RPC batch requests** | Single HTTP round-trip for all headers; most efficient on the wire | Requires dropping from `ethclient` to `rpc.Client.BatchCallContext`; changes the `eth.Client` interface; not all providers handle large batches well |
| **C. Sequential (current)** | Simplest; no concurrency bugs | Slow during catch-up; 100 blocks × 50ms = 5s per batch |

**Decision: Open**

Leaning toward Option A. An `errgroup` with `SetLimit(10)` would cut the 5s down to ~500ms with minimal code change:

```go
g, gCtx := errgroup.WithContext(ctx)
g.SetLimit(10)
results := make([]blockInfo, to-from+1)

for i, bn := range blockRange {
    g.Go(func() error {
        header, err := idx.ethClient.HeaderByNumber(gCtx, ...)
        results[i] = blockInfo{...}
        return err
    })
}
if err := g.Wait(); err != nil { ... }
```

The concurrency limit should be tunable (config or constant) to match the RPC provider's rate limits. Option B is more efficient but adds interface complexity — revisit if Option A still bottlenecks.

---

## 10. Dynamic Contract Watching via DB

**Problem:** The indexer hardcodes watched addresses from config (`FACTORY_ADDRESS`). This means adding a new contract to watch requires a config change and restart. For the factory pattern — where `AuctionCreated` discovers new auction addresses that should also be indexed — addresses need to be added dynamically within the same transaction.

The `WatchedContractRepository` interface and `watched_contracts` table already exist but aren't wired.

**Plan:**

1. Implement `WatchedContractRepository` postgres methods (`GetAddresses`, `Insert`)
2. Seed the factory address into `watched_contracts` on startup or via a seed migration
3. Load addresses from `watched_contracts` in the indexer loop instead of `IndexerConfig.Addresses`
4. Event handlers add new addresses via `txStore.WatchedContractRepo().Insert()` inside `WithTx` — new address is visible on the next batch
5. Remove `Addresses` from `IndexerConfig` and `FACTORY_ADDRESS` from config

**Open question: address refresh frequency**

| Option | Pros | Cons |
|---|---|---|
| **A. Load every batch** | Simplest; new contracts picked up immediately on next iteration; no stale state | Extra DB query per batch (~1ms on a PK-indexed small table); unnecessary when address list rarely changes |
| **B. Cache with TTL refresh** | Fewer DB queries; good for high-frequency polling | Delay before new contracts are picked up; cache invalidation complexity; TTL is arbitrary |
| **C. Cache + invalidate on insert** | Best of both — cached reads, immediate pickup on new addresses | More complex; handler needs to signal the indexer to refresh; tighter coupling between handler and loop |

**Decision: Open**

Leaning toward Option A — the `watched_contracts` table will be tiny (tens of rows at most) and the query is a single-column PK scan. The cost of one cheap query per batch is negligible compared to the RPC calls in the same iteration, and it avoids all caching complexity.

---

## 11. Metrics and Observability

**Problem:** The system only has structured logs. Logs answer "what happened on this request/batch" but are poor at aggregate questions: how far behind is the indexer, is API latency degrading, how often do reorgs happen. Answering these requires parsing logs, which doesn't scale for real-time monitoring or alerting.

**Useful metrics:**

Indexer:
- `indexer_head_block` (gauge) — last indexed block; diff against chain head = "blocks behind"
- `indexer_blocks_processed_total` (counter) — throughput
- `indexer_reorgs_total` (counter) — reorg frequency
- `indexer_batch_duration_seconds` (histogram) — batch processing time
- `indexer_rpc_errors_total` (counter, by operation) — RPC degradation signal

API:
- `http_request_duration_seconds` (histogram, by route + status) — latency percentiles
- `http_requests_total` (counter, by route + status) — request/error rates

**Implementation:** `prometheus/client_golang`, metric variables in the indexer loop and API middleware, `/metrics` endpoint on the API server.

**Decision: Out of scope for now**

Adds a dependency and is dead code without Prometheus/Grafana infrastructure deployed. Revisit when monitoring infrastructure is in place.
