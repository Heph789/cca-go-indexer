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
