# Domain Model

## Source Contracts

The CCA system consists of two deployable contracts and one lens contract:

- **ContinuousClearingAuctionFactory** — deploys auction instances via CREATE2
- **ContinuousClearingAuction** — the main auction contract (inherits BidStorage, CheckpointStorage, StepStorage, TickStorage, TokenCurrencyStorage)
- **AuctionStateLens** — read-only lens for offchain state snapshots via revert-decode pattern

Factory address (v1.1.0): `0xCCccCcCAE7503Cac057829BF2811De42E16e0bD5`
Deployed on: Ethereum Mainnet, Unichain, Base, Sepolia, Unichain Sepolia

Previous factory (v1.0.0): `0x0000ccaDF55C911a2FbC0BB9d2942Aa77c6FAa1D`

## Core Entities

### Auction

Created by the factory. Configured with:

| Field | Type | Description |
|---|---|---|
| currency | address | Payment token (address(0) for ETH) |
| tokensRecipient | address | Receives unsold tokens |
| fundsRecipient | address | Receives raised funds |
| startBlock | uint64 | Auction start |
| endBlock | uint64 | Auction end |
| claimBlock | uint64 | When token claims open (>= endBlock) |
| tickSpacing | uint256 | Price granularity |
| validationHook | address | Optional bid validation |
| floorPrice | uint256 | Minimum price (Q96 encoded) |
| requiredCurrencyRaised | uint128 | Graduation threshold |
| auctionStepsData | bytes | Packed supply schedule |

Derived state: `clearingPrice`, `currencyRaised`, `totalCleared`, `isGraduated`, `totalSupply`.

### Bid

Represents a participant's order in an auction.

| Field | Type | Description |
|---|---|---|
| id | uint256 | Sequential, starting from 0 |
| startBlock | uint64 | Block when bid was submitted |
| startCumulativeMps | uint24 | Snapshot of cumulative mps at submission |
| exitedBlock | uint64 | 0 if not yet exited |
| maxPrice | uint256 | Maximum price willing to pay (Q96) |
| owner | address | Bidder |
| amountQ96 | uint256 | Currency amount (Q96 scaled) |
| tokensFilled | uint256 | Set on exit |

Lifecycle: Submitted → Exited → Tokens Claimed

### Checkpoint

Snapshots of auction state, forming a doubly-linked list indexed by block number. Created once per block that has a new bid (captures state BEFORE that block's bid).

| Field | Type | Description |
|---|---|---|
| clearingPrice | uint256 | Price at this checkpoint |
| currencyRaisedAtClearingPriceQ96_X7 | ValueX7 | Currency raised at clearing price |
| cumulativeMpsPerPrice | uint256 | Harmonic-mean accumulator (mps/price) |
| cumulativeMps | uint24 | Total mps sold so far |
| prev | uint64 | Previous checkpoint block |
| next | uint64 | Next checkpoint block |

### Tick

Price levels forming a singly-linked list of demand.

| Field | Type | Description |
|---|---|---|
| price | uint256 | The price level |
| next | uint256 | Next price in linked list |
| currencyDemandQ96 | uint256 | Cumulative demand at this price |

Sentinel: `MAX_TICK_PTR = type(uint256).max`

### Auction Step

Supply issuance schedule segments. Packed as `bytes8` values (uint24 mps | uint40 blockDelta), stored via SSTORE2.

| Field | Type | Description |
|---|---|---|
| mps | uint24 | Milli-basis-points to sell per block |
| startBlock | uint64 | Step start |
| endBlock | uint64 | Step end |

## Numeric Encoding

- **Q96**: Prices and some currency amounts are fixed-point, scaled by 2^96. To get human-readable: shift right by 96 bits.
- **ValueX7**: Internal accounting values scaled by MPS (1e7) to avoid precision loss.
- **MPS**: 1e7 (10 million milli-basis-points = 100%)

## Auction Lifecycle

```
1. Factory deploys auction          → AuctionCreated
2. Tokens sent to auction           → TokensReceived
3. Bidding phase (startBlock-endBlock)
   - Users submit bids              → BidSubmitted, CheckpointUpdated, ClearingPriceUpdated
   - New price levels created       → TickInitialized
   - Supply schedule advances       → AuctionStepRecorded
4. Auction ends (endBlock)
5. Graduation check                 → isGraduated() if currencyRaised >= requiredCurrencyRaised
6. Exit phase                       → BidExited (tokens filled + currency refunded)
7. Claim phase (after claimBlock)   → TokensClaimed
8. Sweep                            → CurrencySwept, TokensSwept
```
