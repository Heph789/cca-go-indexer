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
