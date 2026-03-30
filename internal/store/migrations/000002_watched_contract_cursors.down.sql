ALTER TABLE event_ccaf_auction_created DROP COLUMN IF EXISTS block_time;
ALTER TABLE indexed_blocks DROP COLUMN IF EXISTS block_time;
ALTER TABLE raw_events DROP COLUMN IF EXISTS block_time;
ALTER TABLE indexer_cursors DROP COLUMN IF EXISTS last_block_time;
ALTER TABLE watched_contracts DROP COLUMN IF EXISTS last_indexed_block;
ALTER TABLE watched_contracts DROP COLUMN IF EXISTS start_block_time;
ALTER TABLE watched_contracts DROP COLUMN IF EXISTS start_block;
