CREATE TABLE event_cca_bid_submitted (
    chain_id        BIGINT      NOT NULL,
    auction_address TEXT        NOT NULL,
    id              NUMERIC     NOT NULL,
    owner           TEXT        NOT NULL,
    price_q96       NUMERIC     NOT NULL,
    amount          NUMERIC     NOT NULL,
    block_number    BIGINT      NOT NULL,
    block_time      TIMESTAMPTZ NOT NULL,
    tx_hash         TEXT        NOT NULL,
    log_index       INTEGER     NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, auction_address, id)
);

CREATE INDEX idx_bid_submitted_owner ON event_cca_bid_submitted (chain_id, auction_address, owner);
CREATE INDEX idx_bid_submitted_block ON event_cca_bid_submitted (chain_id, block_number);
CREATE INDEX idx_bid_submitted_price ON event_cca_bid_submitted (chain_id, auction_address, price_q96);
