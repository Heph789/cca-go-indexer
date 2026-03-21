-- Indexer cursor: per-chain progress tracker.
-- The cursor is the last fully-processed block. On restart, indexing
-- resumes from cursor+1.
CREATE TABLE indexer_cursors (
    chain_id        BIGINT NOT NULL,
    last_block      BIGINT NOT NULL,
    last_block_hash TEXT   NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id)
);

-- Raw events: every log stored as-is for auditing and replay.
-- If a typed event table's schema changes, raw data can re-derive records.
CREATE TABLE raw_events (
    chain_id     BIGINT  NOT NULL,
    block_number BIGINT  NOT NULL,
    block_hash   TEXT    NOT NULL,
    tx_hash      TEXT    NOT NULL,
    log_index    INTEGER NOT NULL,
    address      TEXT    NOT NULL,
    event_name   TEXT    NOT NULL,
    topics       JSONB   NOT NULL,
    data         TEXT    NOT NULL,
    decoded      JSONB,
    indexed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number, tx_hash, log_index)
);

CREATE INDEX idx_raw_events_block ON raw_events (chain_id, block_number);
CREATE INDEX idx_raw_events_event ON raw_events (chain_id, event_name);

-- Auctions: decoded AuctionCreated events.
-- Each event type gets its own table — simple, typed queries, no JSON decoding at read time.
CREATE TABLE auctions (
    chain_id        BIGINT NOT NULL,
    auction_address TEXT   NOT NULL,
    token_out       TEXT   NOT NULL,
    currency_in     TEXT   NOT NULL,
    owner           TEXT   NOT NULL,
    start_time      BIGINT NOT NULL,
    end_time        BIGINT NOT NULL,
    block_number    BIGINT NOT NULL,
    tx_hash         TEXT   NOT NULL,
    log_index       INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, auction_address)
);

-- Indexed blocks: per-block hash storage for reorg detection.
-- Before processing new blocks, the indexer checks that the chain's
-- hash for the previous block matches what's stored here.
CREATE TABLE indexed_blocks (
    chain_id     BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   TEXT   NOT NULL,
    parent_hash  TEXT   NOT NULL,
    indexed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number)
);

-- Watched contracts: addresses the indexer fetches logs for.
-- Factory is seeded on first run. Auction addresses can be added
-- dynamically by event handlers (e.g., AuctionCreated handler adds
-- the new auction address within the same transaction).
CREATE TABLE watched_contracts (
    chain_id BIGINT NOT NULL,
    address  TEXT   NOT NULL,
    label    TEXT   NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, address)
);
