# API Design

## Principles

- REST for CRUD-style reads, with consistent pagination and filtering
- Optional WebSocket/SSE for real-time updates (clearing price, new bids)
- All responses include `chain_id` context
- Prices returned in both raw Q96 and human-readable decimal
- Consistent error format

## Base URL Structure

```
/api/v1/{chain_id}/...
```

Chain ID in the path makes multi-chain explicit and allows chain-specific caching.

## Endpoints

### Auctions

```
GET /api/v1/{chain_id}/auctions
    ?token={address}           # filter by token
    ?status={active|ended|graduated|failed}
    ?cursor={id}&limit={n}     # cursor-based pagination

GET /api/v1/{chain_id}/auctions/{auction_address}

GET /api/v1/{chain_id}/auctions/{auction_address}/state
    # Current state snapshot (clearing price, currency raised, graduation status)
    # Equivalent to what AuctionStateLens provides
```

### Bids

```
GET /api/v1/{chain_id}/auctions/{auction_address}/bids
    ?owner={address}
    ?status={active|exited|claimed}
    ?cursor={id}&limit={n}

GET /api/v1/{chain_id}/auctions/{auction_address}/bids/{bid_id}

GET /api/v1/{chain_id}/owners/{address}/bids
    # All bids by an owner across auctions on this chain
    ?auction={address}         # optional filter
    ?status={active|exited|claimed}
```

### Checkpoints

```
GET /api/v1/{chain_id}/auctions/{auction_address}/checkpoints
    ?from_block={n}&to_block={n}
    ?cursor={block}&limit={n}

GET /api/v1/{chain_id}/auctions/{auction_address}/checkpoints/{block_number}
```

### Price History

```
GET /api/v1/{chain_id}/auctions/{auction_address}/price-history
    ?from_block={n}&to_block={n}
    ?resolution={block|minute|hour}   # for aggregated views
```

### Ticks

```
GET /api/v1/{chain_id}/auctions/{auction_address}/ticks
    # All initialized price levels and their demand
```

### Health & Meta

```
GET /health/live           # process is up
GET /health/ready          # caught up to chain tip (per chain)
GET /api/v1/chains         # list of supported chains with indexer status
GET /metrics               # Prometheus metrics (separate port)
```

## Response Format

```json
{
  "data": { ... },
  "meta": {
    "chain_id": 1,
    "indexed_block": 19500000,
    "chain_tip": 19500005
  }
}
```

For lists:
```json
{
  "data": [ ... ],
  "pagination": {
    "next_cursor": "123",
    "has_more": true
  },
  "meta": { ... }
}
```

## Error Format

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Auction not found at address 0x..."
  }
}
```

## Real-Time (Future Phase)

WebSocket endpoint for streaming updates:

```
WS /api/v1/{chain_id}/auctions/{auction_address}/stream
    # Pushes: new bids, clearing price updates, exits, claims
```

Server-Sent Events as a simpler alternative:

```
GET /api/v1/{chain_id}/auctions/{auction_address}/events
    Accept: text/event-stream
```
