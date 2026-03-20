# Events to Index

## Factory Events

| Event | Parameters | Trigger |
|---|---|---|
| `AuctionCreated(address indexed auction, address indexed token, uint256 amount, bytes configData)` | auction address, token, amount, encoded AuctionParameters | Factory deploys a new auction |

The factory is the entry point for auction discovery. The indexer must track this to dynamically pick up new auction instances.

## Auction Events

| Event | Parameters | Trigger |
|---|---|---|
| `TokensReceived(uint256 totalSupply)` | totalSupply | Auction receives token allocation via `onTokensReceived()` |
| `BidSubmitted(uint256 indexed id, address indexed owner, uint256 price, uint128 amount)` | bidId, owner, maxPrice, amount | `submitBid()` |
| `CheckpointUpdated(uint256 blockNumber, uint256 clearingPrice, uint24 cumulativeMps)` | blockNumber, clearingPrice, cumulativeMps | Checkpoint creation |
| `ClearingPriceUpdated(uint256 blockNumber, uint256 clearingPrice)` | blockNumber, newPrice | Clearing price changes |
| `BidExited(uint256 indexed bidId, address indexed owner, uint256 tokensFilled, uint256 currencyRefunded)` | bidId, owner, tokensFilled, refund | `exitBid()` or `exitPartiallyFilledBid()` |
| `TokensClaimed(uint256 indexed bidId, address indexed owner, uint256 tokensFilled)` | bidId, owner, tokensFilled | `claimTokens()` or `claimTokensBatch()` |

## Tick Events

| Event | Parameters | Trigger |
|---|---|---|
| `TickInitialized(uint256 price)` | price | New price level created |
| `NextActiveTickUpdated(uint256 price)` | price | Next-active-tick pointer changes |

## Step Events

| Event | Parameters | Trigger |
|---|---|---|
| `AuctionStepRecorded(uint256 startBlock, uint256 endBlock, uint24 mps)` | startBlock, endBlock, mps | Advancing to a new auction step |

## Sweep Events

| Event | Parameters | Trigger |
|---|---|---|
| `TokensSwept(address indexed tokensRecipient, uint256 tokensAmount)` | recipient, amount | `sweepUnsoldTokens()` |
| `CurrencySwept(address indexed fundsRecipient, uint256 currencyAmount)` | recipient, amount | `sweepCurrency()` |

## Indexing Strategy

1. **Factory-first**: Poll/subscribe to factory `AuctionCreated` events to discover auctions
2. **Per-auction subscription**: For each discovered auction, index all events above
3. **Event ordering**: Process events in block order, then log index order within a block
4. **Idempotency**: Use (chain_id, block_number, log_index) as a natural dedup key
