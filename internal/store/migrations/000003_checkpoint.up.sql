CREATE TABLE event_cca_checkpoint_updated (
    chain_id         BIGINT      NOT NULL,
    auction_address  TEXT        NOT NULL,
    block_number     BIGINT      NOT NULL,
    clearing_price_q96 NUMERIC   NOT NULL,
    cumulative_mps   INTEGER     NOT NULL,
    tx_block_number  BIGINT      NOT NULL,
    tx_block_time    TIMESTAMPTZ NOT NULL,
    tx_hash          TEXT        NOT NULL,
    log_index        INTEGER     NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, auction_address, block_number)
);

CREATE INDEX idx_checkpoint_block ON event_cca_checkpoint_updated (chain_id, tx_block_number);

COMMENT ON COLUMN event_cca_checkpoint_updated.block_number IS 'Auction logical block from the CheckpointUpdated event parameter, NOT the chain block where the tx was mined (see tx_block_number)';
