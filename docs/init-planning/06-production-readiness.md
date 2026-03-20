# Production Readiness

## Observability

### Structured Logging

Use `log/slog` (stdlib, Go 1.21+). Key conventions:
- JSON format in production, text in development
- Always include: `chain_id`, `block_number`, `component` (indexer/api/store)
- Log at appropriate levels: debug for event processing details, info for lifecycle events, warn for retries, error for failures

### Metrics (Prometheus)

Indexer metrics:
- `indexer_head_block{chain_id}` — latest indexed block (gauge)
- `indexer_chain_tip{chain_id}` — latest chain tip block (gauge)
- `indexer_lag_blocks{chain_id}` — chain_tip - head_block (gauge)
- `indexer_events_processed_total{chain_id, event_type}` — counter
- `indexer_reorgs_total{chain_id}` — counter
- `indexer_rpc_requests_total{chain_id, method, status}` — counter
- `indexer_rpc_latency_seconds{chain_id, method}` — histogram
- `indexer_block_processing_seconds{chain_id}` — histogram

API metrics:
- `api_requests_total{method, path, status}` — counter
- `api_request_duration_seconds{method, path}` — histogram
- `api_active_connections` — gauge

Database metrics:
- `db_query_duration_seconds{query}` — histogram
- `db_pool_active_connections` — gauge
- `db_pool_idle_connections` — gauge

### Health Checks

- **Liveness** (`/health/live`): process is running, not deadlocked. Always returns 200 unless something is fundamentally broken.
- **Readiness** (`/health/ready`): indexer is within acceptable lag of chain tip, database is reachable. Returns 503 if not ready.

## Reliability

### Graceful Shutdown

- Trap SIGINT/SIGTERM
- Propagate cancellation via `context.Context`
- Drain in-flight API requests (HTTP server `Shutdown()`)
- Finish current indexer block before stopping (don't leave partial state)
- Close database connections cleanly

### RPC Client Resilience

- Retry with exponential backoff for transient errors (rate limits, timeouts)
- Circuit breaker pattern for sustained RPC failures
- Support multiple RPC endpoints per chain with fallback
- Rate limiting to stay within provider limits

### Database Resilience

- Connection pooling via pgxpool
- Retry transient errors (connection reset, serialization failure)
- All event processing within a transaction per block (atomic block processing)
- Migrations run at startup with a lock to prevent concurrent migration

## Security

### API

- Rate limiting per IP (middleware)
- Request size limits
- Input validation on all path/query parameters (addresses, block numbers)
- No write endpoints — this is a read-only API
- CORS configuration for frontend consumers

### Infrastructure

- Database credentials via environment variables, never in code or config files
- RPC API keys via environment variables
- No secrets in Docker images

## Testing

### Unit Tests

- Domain logic (Q96 math, event parsing, status derivation)
- Handler logic with mocked stores

### Integration Tests

- Database tests with testcontainers-go (real PostgreSQL)
- Event processing pipeline: raw event → store → query → verify
- Reorg simulation tests

### End-to-End

- Docker Compose with a local Anvil/Hardhat fork
- Deploy contracts, submit bids, verify indexed data matches chain state

## Deployment

### Docker

- Multi-stage build (builder + minimal runtime)
- Non-root user
- Health check in Dockerfile

### Configuration

All via environment variables (12-factor):

| Variable | Description | Default |
|---|---|---|
| `DATABASE_URL` | PostgreSQL connection string | required |
| `CHAINS` | Comma-separated chain IDs to index | required |
| `RPC_URL_{CHAIN_ID}` | RPC endpoint per chain | required |
| `START_BLOCK_{CHAIN_ID}` | Starting block for indexing | factory deploy block |
| `LOG_LEVEL` | Logging level | info |
| `LOG_FORMAT` | json or text | json |
| `API_PORT` | HTTP listen port | 8080 |
| `METRICS_PORT` | Prometheus metrics port | 9090 |
| `POLL_INTERVAL` | Indexer poll interval | 2s |
| `FINALITY_DEPTH_{CHAIN_ID}` | Blocks to wait for finality | chain-dependent |
