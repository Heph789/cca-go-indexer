# simulate/chain

Local blockchain simulation for the CCA Go Indexer.

## What it does

1. Deploys the `ContinuousClearingAuctionFactory` contract to a local Anvil chain
2. Deploys a mock ERC20 token
3. Creates a test auction via `initializeDistribution`, emitting an `AuctionCreated` event

The indexer can then be pointed at the local chain to pick up and process this event.

## Prerequisites

- [Foundry](https://book.getfoundry.sh/getting-started/installation) (provides `anvil` and `forge`)
- Reference submodule initialized: `git submodule update --init --recursive`

## Usage

```bash
# Terminal 1: Start Anvil
anvil

# Terminal 2: Deploy contracts
./deploy.sh
```

The deploy script uses Anvil's default account 0 private key. The factory address and auction address are printed to stdout.

## Configuring the indexer

After deploying, set these env vars to point the indexer at the local chain:

```
RPC_URL=http://127.0.0.1:8545
CHAIN_ID=31337
FACTORY_ADDRESS=<factory address from deploy output>
START_BLOCK=0
CONFIRMATIONS=0
POLL_INTERVAL=1s
```
