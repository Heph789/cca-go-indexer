package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Shared
	DatabaseURL     string
	DatabaseReadURL string
	ChainID         int64
	LogLevel        string
	LogFormat       string
	Port            string

	// Indexer-specific
	RPCURL         string
	FactoryAddr    string
	StartBlock     uint64
	PollInterval   time.Duration
	BlockBatchSize uint64
	Confirmations  uint64
}

func loadBase() (*Config, error) {
	cfg := &Config{}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.DatabaseReadURL = os.Getenv("DATABASE_READ_URL")
	if cfg.DatabaseReadURL == "" {
		cfg.DatabaseReadURL = cfg.DatabaseURL
	}

	chainStr := os.Getenv("CHAIN_ID")
	if chainStr != "" {
		id, err := strconv.ParseInt(chainStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing CHAIN_ID: %w", err)
		}
		cfg.ChainID = id
	}

	cfg.LogLevel = envOrDefault("LOG_LEVEL", "info")
	cfg.LogFormat = envOrDefault("LOG_FORMAT", "json")
	cfg.Port = envOrDefault("PORT", "8080")

	cfg.RPCURL = os.Getenv("RPC_URL")
	cfg.FactoryAddr = os.Getenv("FACTORY_ADDRESS")

	pollStr := envOrDefault("POLL_INTERVAL", "12s")
	poll, err := time.ParseDuration(pollStr)
	if err != nil {
		return nil, fmt.Errorf("parsing POLL_INTERVAL: %w", err)
	}
	cfg.PollInterval = poll

	cfg.BlockBatchSize, err = parseUint64Env("BLOCK_BATCH_SIZE", 100)
	if err != nil {
		return nil, err
	}

	cfg.Confirmations, err = parseUint64Env("CONFIRMATIONS", 0)
	if err != nil {
		return nil, err
	}

	cfg.StartBlock, err = parseUint64Env("START_BLOCK", 0)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func LoadAPI() (*Config, error) {
	cfg, err := loadBase()
	if err != nil {
		return nil, err
	}
	if err := cfg.validateBase(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadIndexer() (*Config, error) {
	cfg, err := loadBase()
	if err != nil {
		return nil, err
	}
	if err := cfg.validateBase(); err != nil {
		return nil, err
	}
	if cfg.RPCURL == "" {
		return nil, fmt.Errorf("RPC_URL is required")
	}
	if cfg.FactoryAddr == "" {
		return nil, fmt.Errorf("FACTORY_ADDRESS is required")
	}
	return cfg, nil
}

func (c *Config) validateBase() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.ChainID == 0 {
		return fmt.Errorf("CHAIN_ID is required")
	}
	return nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseUint64Env(key string, defaultVal uint64) (uint64, error) {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal, nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", key, err)
	}
	return v, nil
}
