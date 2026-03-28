CREATE TABLE indexer_cursors (
    chain_id        BIGINT NOT NULL,
    last_block      BIGINT NOT NULL,
    last_block_hash TEXT   NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id)
);

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
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number, tx_hash, log_index)
);

CREATE INDEX idx_raw_events_block ON raw_events (chain_id, block_number);
CREATE INDEX idx_raw_events_event ON raw_events (chain_id, event_name);

CREATE TABLE event_ccaf_auction_created (
    chain_id                 BIGINT  NOT NULL,
    auction_address          TEXT    NOT NULL,
    token                    TEXT    NOT NULL,
    amount                   NUMERIC NOT NULL,
    currency                 TEXT    NOT NULL,
    tokens_recipient         TEXT    NOT NULL,
    funds_recipient          TEXT    NOT NULL,
    start_block              BIGINT  NOT NULL,
    end_block                BIGINT  NOT NULL,
    claim_block              BIGINT  NOT NULL,
    tick_spacing             NUMERIC NOT NULL,
    validation_hook          TEXT    NOT NULL,
    floor_price              NUMERIC NOT NULL,
    required_currency_raised NUMERIC NOT NULL,
    block_number             BIGINT  NOT NULL,
    tx_hash                  TEXT    NOT NULL,
    log_index                INTEGER NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, auction_address)
);

CREATE TABLE indexed_blocks (
    chain_id     BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   TEXT   NOT NULL,
    parent_hash  TEXT   NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number)
);

CREATE TABLE watched_contracts (
    chain_id   BIGINT NOT NULL,
    address    TEXT   NOT NULL,
    label      TEXT   NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, address)
);
