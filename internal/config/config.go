package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cca/go-indexer/internal/eth"
	"github.com/joho/godotenv"
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
	RPCURL            string
	FactoryAddr       string
	StartBlock        uint64
	PollInterval      time.Duration
	BlockBatchSize    uint64
	Confirmations     uint64
	HeaderConcurrency int
	Retry             eth.RetryConfig
}

func loadBase() (*Config, error) {
	// Load .env file if present; no error if missing.
	_ = godotenv.Load()

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

	var err error
	cfg.PollInterval, err = parseDurationEnv("POLL_INTERVAL", "12s")
	if err != nil {
		return nil, err
	}

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

	cfg.HeaderConcurrency, err = parseIntEnv("HEADER_CONCURRENCY", 1)
	if err != nil {
		return nil, err
	}

	cfg.Retry.MaxRetries, err = parseIntEnv("RETRY_MAX_RETRIES", 5)
	if err != nil {
		return nil, err
	}

	cfg.Retry.BaseDelay, err = parseDurationEnv("RETRY_BASE_DELAY", "500ms")
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

func parseIntEnv(key string, defaultVal int) (int, error) {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", key, err)
	}
	return v, nil
}

func parseDurationEnv(key, defaultVal string) (time.Duration, error) {
	s := envOrDefault(key, defaultVal)
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", key, err)
	}
	return d, nil
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
