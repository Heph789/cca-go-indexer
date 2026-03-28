# simulate/resilience

Resilience QA simulations for the CCA Go Indexer. Tests failure recovery, retry budget exhaustion, and chain reorg rollback.

## Scenarios

| Script | What it tests |
|--------|--------------|
| `run_retry_recovery.py` | Indexer recovers from a transient RPC outage via transport retries |
| `run_retry_budget.py` | Indexer exits cleanly after exhausting its retry budget (5 consecutive loop failures) |
| `run_reorg_rollback.py` | Reorg rollback deletes stale data and re-indexes the new canonical chain |

## Prerequisites

- [Foundry](https://book.getfoundry.sh/getting-started/installation) (`anvil`, `forge`)
- Go 1.22+
- PostgreSQL running and accessible
- `psql` CLI available
- Python 3.10+
- Reference submodule initialized: `git submodule update --init --recursive`

## Usage

```bash
# Run all scenarios
python3 simulate/resilience/run_all.py

# Run a single scenario
python3 simulate/resilience/run_retry_recovery.py
python3 simulate/resilience/run_retry_budget.py
python3 simulate/resilience/run_reorg_rollback.py
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable` | Postgres connection string |
| `ANVIL_PORT` | `8545` | Anvil RPC port |
| `PROXY_PORT` | `9545` | Fault proxy port |

## Architecture

```
Indexer  -->  Fault Proxy (faultproxy.py)  -->  Anvil
                  ^
                  |  /_fault/on   (return 503)
                  |  /_fault/off  (resume proxying)
                  |  /_fault/status
```

The fault proxy sits between the indexer and Anvil. Test scripts toggle fault injection via HTTP control endpoints to simulate RPC outages. For the reorg test, the indexer connects directly to Anvil, which uses `evm_snapshot` / `evm_revert` to simulate chain reorganizations.

## How it works

### Retry Recovery
1. Start Anvil + proxy + indexer, let indexer catch up
2. Enable fault injection (proxy returns 503)
3. Mine blocks on Anvil (indexer can't see them)
4. Disable fault injection
5. Verify indexer catches up and is still running

### Retry Budget Exhaustion
1. Start Anvil + proxy + indexer, let indexer catch up
2. Enable permanent fault injection
3. Wait for indexer to exit after 5 consecutive failures
4. Verify exit code is non-zero and DB is unchanged

### Reorg Rollback
1. Start Anvil + indexer, let indexer catch up
2. Take EVM snapshot, mine 5 blocks, let indexer index them
3. Revert to snapshot, mine new blocks (different hashes at same heights)
4. Verify indexer detected reorg, rolled back stale data, and re-indexed correctly
