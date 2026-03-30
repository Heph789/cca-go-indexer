-- Add per-contract cursor columns to watched_contracts for backfill tracking.
ALTER TABLE watched_contracts ADD COLUMN start_block BIGINT NOT NULL DEFAULT 0;
ALTER TABLE watched_contracts ADD COLUMN start_block_time TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch';
ALTER TABLE watched_contracts ADD COLUMN last_indexed_block BIGINT NOT NULL DEFAULT 0;

-- Add block_time to infrastructure tables so downstream consumers
-- (event handlers, GraphQL resolvers) can surface wall-clock times.
ALTER TABLE indexer_cursors ADD COLUMN last_block_time TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch';
ALTER TABLE raw_events ADD COLUMN block_time TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch';
ALTER TABLE indexed_blocks ADD COLUMN block_time TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch';
ALTER TABLE event_ccaf_auction_created ADD COLUMN block_time TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch';
