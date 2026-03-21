// Package config loads indexer configuration from environment variables.
// No external config libraries — just os.Getenv with sensible defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL    string
	RPCURL         string
	ChainID        int64
	FactoryAddr    string
	StartBlock     uint64
	PollInterval   time.Duration
	BlockBatchSize uint64
	MaxBlockRange  uint64
	Confirmations  uint64
	RPCMaxRetries  int
	RPCRetryDelay  time.Duration
	LogLevel       string
	LogFormat      string
}

// Load reads config from environment variables.
// Required: DATABASE_URL, RPC_URL, CHAIN_ID, FACTORY_ADDRESS.
// Returns an error if any required field is missing.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RPCURL:      os.Getenv("RPC_URL"),
		FactoryAddr: os.Getenv("FACTORY_ADDRESS"),
		LogLevel:    envOrDefault("LOG_LEVEL", "info"),
		LogFormat:   envOrDefault("LOG_FORMAT", "json"),
	}

	// --- Required fields ---
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RPCURL == "" {
		return nil, fmt.Errorf("RPC_URL is required")
	}
	if cfg.FactoryAddr == "" {
		return nil, fmt.Errorf("FACTORY_ADDRESS is required")
	}

	// --- Numeric fields ---
	var err error
	cfg.ChainID, err = requiredInt64("CHAIN_ID")
	if err != nil {
		return nil, err
	}

	cfg.StartBlock = envOrDefaultUint64("START_BLOCK", 0)
	cfg.BlockBatchSize = envOrDefaultUint64("BLOCK_BATCH_SIZE", 100)
	cfg.MaxBlockRange = envOrDefaultUint64("MAX_BLOCK_RANGE", 2000)
	cfg.Confirmations = envOrDefaultUint64("CONFIRMATIONS", 0)
	cfg.RPCMaxRetries = envOrDefaultInt("RPC_MAX_RETRIES", 3)

	// --- Duration fields ---
	pollStr := envOrDefault("POLL_INTERVAL", "12s")
	cfg.PollInterval, err = time.ParseDuration(pollStr)
	if err != nil {
		return nil, fmt.Errorf("invalid POLL_INTERVAL: %w", err)
	}

	retryDelayStr := envOrDefault("RPC_RETRY_DELAY", "250ms")
	cfg.RPCRetryDelay, err = time.ParseDuration(retryDelayStr)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_RETRY_DELAY: %w", err)
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envOrDefaultUint64(key string, fallback uint64) uint64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func requiredInt64(key string) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	return strconv.ParseInt(v, 10, 64)
}
