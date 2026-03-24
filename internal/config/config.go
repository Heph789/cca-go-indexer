// Package config loads indexer configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the indexer.
type Config struct {
	DatabaseURL    string
	RPCURL         string
	ChainID        int64
	FactoryAddress string
	StartBlock     uint64
	PollInterval   time.Duration
	BlockBatchSize uint64
	Confirmations  uint64
	LogLevel       string
}

// Load reads configuration from environment variables and returns a Config.
// Required variables (DATABASE_URL, RPC_URL, CHAIN_ID, FACTORY_ADDRESS) must
// be set to non-empty values or an error is returned.
func Load() (*Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		return nil, fmt.Errorf("RPC_URL is required")
	}

	chainIDStr := os.Getenv("CHAIN_ID")
	if chainIDStr == "" {
		return nil, fmt.Errorf("CHAIN_ID is required")
	}
	chainID, err := strconv.ParseInt(chainIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("CHAIN_ID: %w", err)
	}

	factoryAddress := os.Getenv("FACTORY_ADDRESS")
	if factoryAddress == "" {
		return nil, fmt.Errorf("FACTORY_ADDRESS is required")
	}

	startBlock, err := parseUint64WithDefault("START_BLOCK", "0")
	if err != nil {
		return nil, fmt.Errorf("START_BLOCK: %w", err)
	}

	pollIntervalStr := envOrDefault("POLL_INTERVAL", "12s")
	pollInterval, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		return nil, fmt.Errorf("POLL_INTERVAL: %w", err)
	}

	blockBatchSize, err := parseUint64WithDefault("BLOCK_BATCH_SIZE", "100")
	if err != nil {
		return nil, fmt.Errorf("BLOCK_BATCH_SIZE: %w", err)
	}

	confirmations, err := parseUint64WithDefault("CONFIRMATIONS", "12")
	if err != nil {
		return nil, fmt.Errorf("CONFIRMATIONS: %w", err)
	}

	logLevel := envOrDefault("LOG_LEVEL", "info")

	return &Config{
		DatabaseURL:    databaseURL,
		RPCURL:         rpcURL,
		ChainID:        chainID,
		FactoryAddress: factoryAddress,
		StartBlock:     startBlock,
		PollInterval:   pollInterval,
		BlockBatchSize: blockBatchSize,
		Confirmations:  confirmations,
		LogLevel:       logLevel,
	}, nil
}

// envOrDefault returns the environment variable value or the default if unset/empty.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// parseUint64WithDefault reads an env var as uint64, using the default if unset/empty.
func parseUint64WithDefault(key, defaultVal string) (uint64, error) {
	s := envOrDefault(key, defaultVal)
	return strconv.ParseUint(s, 10, 64)
}
