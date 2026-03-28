# Production Readiness QA

Verification experiments for **Phase 3: Production Readiness** ([#45](https://github.com/cca/go-indexer/issues/45)).

## What This Phase Verifies

Phase 3 adds no new business logic. It verifies the operational layer that makes the API production-ready:

- **Middleware chain** — CORS, X-Request-ID, panic recovery, request logging
- **Health and readiness probes** — `/health` (liveness), `/ready` (readiness with DB ping)
- **Cache-Control headers** — `public, max-age=86400, immutable` on immutable auction data; `no-store` on probes
- **All prior phase tests still pass** — no regressions

## Experiments

| Script | What it tests |
|--------|---------------|
| `run_middleware.py` | CORS headers, X-Request-ID propagation, OPTIONS preflight, panic recovery, CORS on error responses |
| `run_probes.py` | Health always 200, readiness 200/503 based on DB state, readiness flap recovery, no-store headers |
| `run_cache.py` | Cache-Control on 200, absent on errors (404/400), directive validation, no cache leak across requests, response immutability |
| `run_all.py` | Orchestrates all experiments sequentially |

## Prerequisites

- Go 1.22+
- PostgreSQL running locally
- Foundry installed (anvil, forge)
- Python 3.10+

## Usage

```bash
# Run all production readiness experiments
cd simulate/production
python3 run_all.py

# Run a single experiment group
python3 run_middleware.py
python3 run_probes.py
python3 run_cache.py
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable` | Postgres connection |
| `API_PORT` | `8080` | API server port |
| `ANVIL_PORT` | `8545` | Anvil RPC port |

## Red Phase

Tests are designed to **fail** against the resilience QA branch (`indexer-api-happy-1-/qa-resilience-1`) because that branch has no middleware, no readiness probe, and no cache headers. All failures are runtime (wrong/missing headers, wrong status codes), not compilation errors.
