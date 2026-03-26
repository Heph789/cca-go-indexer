# simulate

Local simulation environment for testing the CCA Go Indexer end-to-end without external dependencies.

## Overview

```
simulate/
  chain/     Local Anvil blockchain with deployed CCA contracts
  e2e/       End-to-end orchestration and verification scripts

reference/
  continuous-clearing-auction/   Git submodule of the CCA Solidity contracts
```

The chain simulation deploys the real `ContinuousClearingAuctionFactory` contract to a local Anvil instance, creates a test auction (emitting the `AuctionCreated` event), and funds it with mock ERC20 tokens. The indexer and API can then be run against this local chain and verified.

## Prerequisites

- [Foundry](https://book.getfoundry.sh/getting-started/installation) (provides `anvil` and `forge`)
- Go 1.22+
- PostgreSQL running locally

## Quick start

### 1. Initialize submodules

```bash
git submodule update --init --recursive
```

### 2. Automated (full e2e)

Runs everything — Anvil, contract deployment, indexer, API — and verifies the result:

```bash
cd simulate/e2e
./run.sh
```

Requires Postgres to be running. Prints `QA PASSED` or `QA FAILED` on completion.

### 3. Manual (step by step)

If you prefer to run each component separately:

```bash
# Terminal 1: Start Anvil
anvil

# Terminal 2: Deploy contracts
cd simulate/chain
./deploy.sh
# Note the Factory and Auction addresses from the output

# Terminal 3: Run the indexer
DATABASE_URL=postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable \
RPC_URL=http://127.0.0.1:8545 \
CHAIN_ID=31337 \
FACTORY_ADDRESS=<factory address from deploy output> \
START_BLOCK=0 \
CONFIRMATIONS=0 \
POLL_INTERVAL=1s \
BLOCK_BATCH_SIZE=100 \
MAX_BLOCK_RANGE=2000 \
LOG_LEVEL=info \
LOG_FORMAT=text \
go run ./cmd/indexer

# Terminal 4: Run the API
DATABASE_URL=postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable \
CHAIN_ID=31337 \
PORT=8080 \
LOG_LEVEL=info \
LOG_FORMAT=text \
go run ./cmd/api

# Terminal 5: Verify
cd simulate/e2e
./verify.sh <auction address from deploy output>
```

## How it works

### Chain simulation (`simulate/chain/`)

A Foundry project that compiles against the real CCA contracts (via the `reference/` submodule). The `DeployAndCreateAuction.s.sol` script:

1. Deploys the `ContinuousClearingAuctionFactory`
2. Deploys a mock ERC20 token and mints 1M tokens
3. Calls `factory.initializeDistribution()` to create an auction (emits `AuctionCreated`)
4. Transfers tokens to the auction and calls `onTokensReceived()`

Uses Anvil's default account 0 (`0xf39F...2266`) with its well-known private key.

### E2E verification (`simulate/e2e/`)

- `run.sh` — Orchestrates the full flow: starts Anvil, deploys, starts indexer + API, waits, then runs verification
- `verify.sh` — Standalone assertions: checks the health endpoint and verifies `GET /api/v1/auctions/{address}` returns correct data
