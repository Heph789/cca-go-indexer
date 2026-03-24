-- +goose Up
CREATE TABLE IF NOT EXISTS cursors (
	chain_id BIGINT PRIMARY KEY,
	block_number BIGINT NOT NULL DEFAULT 0,
	block_hash TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS blocks (
	chain_id BIGINT NOT NULL,
	block_number BIGINT NOT NULL,
	block_hash TEXT NOT NULL,
	parent_hash TEXT NOT NULL,
	PRIMARY KEY (chain_id, block_number)
);

CREATE TABLE IF NOT EXISTS raw_events (
	chain_id BIGINT NOT NULL,
	block_number BIGINT NOT NULL,
	block_hash TEXT NOT NULL,
	tx_hash TEXT NOT NULL,
	log_index INT NOT NULL,
	address TEXT NOT NULL,
	event_name TEXT NOT NULL,
	topics_json TEXT NOT NULL,
	data_hex TEXT NOT NULL,
	decoded_json TEXT NOT NULL,
	indexed_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (chain_id, block_number, log_index)
);

CREATE TABLE IF NOT EXISTS auctions (
	auction_address TEXT NOT NULL,
	token TEXT NOT NULL,
	total_supply NUMERIC NOT NULL,
	currency TEXT NOT NULL,
	tokens_recipient TEXT NOT NULL,
	funds_recipient TEXT NOT NULL,
	start_block BIGINT NOT NULL,
	end_block BIGINT NOT NULL,
	claim_block BIGINT NOT NULL,
	tick_spacing_q96 NUMERIC NOT NULL,
	validation_hook TEXT NOT NULL,
	floor_price_q96 NUMERIC NOT NULL,
	required_currency_raised NUMERIC NOT NULL,
	auction_steps_data BYTEA NOT NULL,
	emitter_contract TEXT NOT NULL,
	chain_id BIGINT NOT NULL,
	block_number BIGINT NOT NULL,
	tx_hash TEXT NOT NULL,
	log_index INT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (chain_id, block_number, log_index)
);

-- +goose Down
DROP TABLE IF EXISTS auctions;
DROP TABLE IF EXISTS raw_events;
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS cursors;
