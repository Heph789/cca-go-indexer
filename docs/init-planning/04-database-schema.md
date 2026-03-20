# Database Schema

## Design Principles

- `chain_id` on every table to support multi-chain in a single database
- `NUMERIC` for big integers (uint256) — avoids precision loss
- Store both raw Q96 values and decoded human-readable values where useful for queries
- `(chain_id, block_number, log_index)` as natural keys for event dedup
- Timestamps derived from block timestamps for time-based queries
- Indexes on common query patterns (by auction, by owner, by status)

## Tables

### raw_events

Append-only log of all indexed events. Enables replay and debugging.

```sql
CREATE TABLE raw_events (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        INTEGER NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    tx_hash         BYTEA NOT NULL,
    log_index       INTEGER NOT NULL,
    contract_addr   BYTEA NOT NULL,
    event_name      TEXT NOT NULL,
    event_data      JSONB NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chain_id, block_number, tx_hash, log_index)
);

CREATE INDEX idx_raw_events_contract ON raw_events (chain_id, contract_addr, event_name);
CREATE INDEX idx_raw_events_block ON raw_events (chain_id, block_number);
```

### indexer_cursors

Tracks indexing progress per chain.

```sql
CREATE TABLE indexer_cursors (
    chain_id        INTEGER PRIMARY KEY,
    last_block      BIGINT NOT NULL,
    last_block_hash BYTEA NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### auctions

```sql
CREATE TABLE auctions (
    id                      BIGSERIAL PRIMARY KEY,
    chain_id                INTEGER NOT NULL,
    auction_address         BYTEA NOT NULL,
    token_address           BYTEA NOT NULL,
    currency_address        BYTEA NOT NULL,
    tokens_recipient        BYTEA NOT NULL,
    funds_recipient         BYTEA NOT NULL,
    start_block             BIGINT NOT NULL,
    end_block               BIGINT NOT NULL,
    claim_block             BIGINT NOT NULL,
    tick_spacing            NUMERIC NOT NULL,
    validation_hook         BYTEA,
    floor_price_q96         NUMERIC NOT NULL,
    required_currency_raised NUMERIC NOT NULL,
    total_supply            NUMERIC,              -- set on TokensReceived
    clearing_price_q96      NUMERIC,              -- updated on ClearingPriceUpdated
    currency_raised         NUMERIC DEFAULT 0,    -- updated from events
    is_graduated            BOOLEAN DEFAULT FALSE,
    created_block           BIGINT NOT NULL,
    created_tx_hash         BYTEA NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL,
    UNIQUE (chain_id, auction_address)
);

CREATE INDEX idx_auctions_token ON auctions (chain_id, token_address);
```

### bids

```sql
CREATE TABLE bids (
    id                      BIGSERIAL PRIMARY KEY,
    chain_id                INTEGER NOT NULL,
    auction_address         BYTEA NOT NULL,
    bid_id                  NUMERIC NOT NULL,       -- on-chain sequential ID
    owner                   BYTEA NOT NULL,
    max_price_q96           NUMERIC NOT NULL,
    amount_q96              NUMERIC NOT NULL,
    start_block             BIGINT NOT NULL,
    start_cumulative_mps    INTEGER NOT NULL,
    exited_block            BIGINT,                 -- NULL if not exited
    tokens_filled           NUMERIC,                -- set on exit
    currency_refunded       NUMERIC,                -- set on exit
    tokens_claimed          BOOLEAN DEFAULT FALSE,
    submitted_tx_hash       BYTEA NOT NULL,
    submitted_at            TIMESTAMPTZ NOT NULL,
    exited_tx_hash          BYTEA,
    claimed_tx_hash         BYTEA,
    UNIQUE (chain_id, auction_address, bid_id)
);

CREATE INDEX idx_bids_auction ON bids (chain_id, auction_address);
CREATE INDEX idx_bids_owner ON bids (chain_id, owner);
CREATE INDEX idx_bids_status ON bids (chain_id, auction_address) WHERE exited_block IS NULL;
```

### checkpoints

```sql
CREATE TABLE checkpoints (
    id                                      BIGSERIAL PRIMARY KEY,
    chain_id                                INTEGER NOT NULL,
    auction_address                         BYTEA NOT NULL,
    block_number                            BIGINT NOT NULL,
    clearing_price_q96                      NUMERIC NOT NULL,
    currency_raised_at_clearing_price_q96   NUMERIC NOT NULL,
    cumulative_mps_per_price                NUMERIC NOT NULL,
    cumulative_mps                          INTEGER NOT NULL,
    prev_block                              BIGINT,
    next_block                              BIGINT,
    UNIQUE (chain_id, auction_address, block_number)
);
```

### ticks

```sql
CREATE TABLE ticks (
    id                  BIGSERIAL PRIMARY KEY,
    chain_id            INTEGER NOT NULL,
    auction_address     BYTEA NOT NULL,
    price_q96           NUMERIC NOT NULL,
    currency_demand_q96 NUMERIC NOT NULL,
    initialized_block   BIGINT NOT NULL,
    UNIQUE (chain_id, auction_address, price_q96)
);
```

### auction_steps

```sql
CREATE TABLE auction_steps (
    id              BIGSERIAL PRIMARY KEY,
    chain_id        INTEGER NOT NULL,
    auction_address BYTEA NOT NULL,
    step_index      INTEGER NOT NULL,
    mps             INTEGER NOT NULL,
    start_block     BIGINT NOT NULL,
    end_block       BIGINT NOT NULL,
    UNIQUE (chain_id, auction_address, step_index)
);
```

### block_hashes

For reorg detection. Stores recent block hashes per chain.

```sql
CREATE TABLE block_hashes (
    chain_id     INTEGER NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   BYTEA NOT NULL,
    PRIMARY KEY (chain_id, block_number)
);
```

## Reorg Rollback

On reorg detection, delete in reverse dependency order:

```sql
DELETE FROM raw_events WHERE chain_id = $1 AND block_number > $2;
DELETE FROM bids WHERE chain_id = $1 AND start_block > $2;
DELETE FROM checkpoints WHERE chain_id = $1 AND block_number > $2;
DELETE FROM ticks WHERE chain_id = $1 AND initialized_block > $2;
DELETE FROM auction_steps WHERE chain_id = $1 AND start_block > $2;
DELETE FROM block_hashes WHERE chain_id = $1 AND block_number > $2;
-- Auctions created after the reorg point also need removal
DELETE FROM auctions WHERE chain_id = $1 AND created_block > $2;
-- Reset cursor
UPDATE indexer_cursors SET last_block = $2, last_block_hash = $3 WHERE chain_id = $1;
```

Note: Bids that were exited or claimed after the reorg point but submitted before it need their exit/claim fields reset rather than full deletion. This requires more nuanced handling.
