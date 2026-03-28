# simulate/e2e

End-to-end QA orchestration for the CCA Go Indexer.

## What it does

`run.sh` automates the full happy-path verification:

1. Starts a local Anvil chain
2. Deploys the CCA factory and creates a test auction (emitting `AuctionCreated`)
3. Starts the Go indexer pointed at the local chain
4. Starts the Go API server
5. Runs `verify.sh` to assert the API returns the indexed auction

## Prerequisites

- [Foundry](https://book.getfoundry.sh/getting-started/installation) (`anvil`, `forge`)
- Go 1.22+
- Postgres running and accessible (default: `postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable`)
- Reference submodule initialized: `git submodule update --init --recursive`

## Usage

```bash
# Run the full QA suite
./run.sh

# Or run just the verification against an already-running setup
./verify.sh <auction_address> [api_port]
```

## Environment variables

| Variable       | Default                                                    | Description                |
|----------------|------------------------------------------------------------|----------------------------|
| `DATABASE_URL` | `postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable` | Postgres connection string |
| `API_PORT`     | `8080`                                                     | API server port            |
| `ANVIL_PORT`   | `8545`                                                     | Anvil RPC port             |
