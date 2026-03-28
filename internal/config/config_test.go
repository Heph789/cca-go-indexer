package config

import (
	"testing"
	"time"
)

func TestLoadIndexer_MissingDatabaseURL(t *testing.T) {
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0x1234567890abcdef1234567890abcdef12345678")

	_, err := LoadIndexer()
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}

func TestLoadIndexer_MissingChainID(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0x1234567890abcdef1234567890abcdef12345678")

	_, err := LoadIndexer()
	if err == nil {
		t.Fatal("expected error when CHAIN_ID is missing")
	}
}

func TestLoadIndexer_MissingRPCURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("FACTORY_ADDRESS", "0x1234567890abcdef1234567890abcdef12345678")

	_, err := LoadIndexer()
	if err == nil {
		t.Fatal("expected error when RPC_URL is missing")
	}
}

func TestLoadIndexer_MissingFactoryAddress(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")

	_, err := LoadIndexer()
	if err == nil {
		t.Fatal("expected error when FACTORY_ADDRESS is missing")
	}
}

func TestLoadAPI_MissingDatabaseURL(t *testing.T) {
	t.Setenv("CHAIN_ID", "324")

	_, err := LoadAPI()
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}

func TestLoadAPI_MissingChainID(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")

	_, err := LoadAPI()
	if err == nil {
		t.Fatal("expected error when CHAIN_ID is missing")
	}
}

func TestLoadAPI_DoesNotRequireRPCOrFactory(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/test" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://localhost/test")
	}
	if cfg.ChainID != 324 {
		t.Errorf("ChainID = %d, want 324", cfg.ChainID)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0xfactory")

	cfg, err := LoadIndexer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
	if cfg.PollInterval != 12*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 12*time.Second)
	}
	if cfg.BlockBatchSize != 100 {
		t.Errorf("BlockBatchSize = %d, want 100", cfg.BlockBatchSize)
	}
	if cfg.Confirmations != 0 {
		t.Errorf("Confirmations = %d, want 0", cfg.Confirmations)
	}
	if cfg.StartBlock != 0 {
		t.Errorf("StartBlock = %d, want 0", cfg.StartBlock)
	}
	if cfg.HeaderConcurrency != 1 {
		t.Errorf("HeaderConcurrency = %d, want 1", cfg.HeaderConcurrency)
	}
	if cfg.RetryMaxRetries != 5 {
		t.Errorf("RetryMaxRetries = %d, want 5", cfg.RetryMaxRetries)
	}
	if cfg.RetryBaseDelay != 500*time.Millisecond {
		t.Errorf("RetryBaseDelay = %v, want %v", cfg.RetryBaseDelay, 500*time.Millisecond)
	}
}

func TestLoad_ParsesHeaderConcurrency(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0xfactory")
	t.Setenv("HEADER_CONCURRENCY", "8")

	cfg, err := LoadIndexer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HeaderConcurrency != 8 {
		t.Errorf("HeaderConcurrency = %d, want 8", cfg.HeaderConcurrency)
	}
}

func TestLoad_ParsesPollInterval(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0xfactory")
	t.Setenv("POLL_INTERVAL", "5s")

	cfg, err := LoadIndexer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 5*time.Second)
	}
}

func TestLoad_DatabaseReadURLFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DatabaseReadURL != "postgres://localhost/test" {
		t.Errorf("DatabaseReadURL = %q, want %q", cfg.DatabaseReadURL, "postgres://localhost/test")
	}
}

func TestLoad_DatabaseReadURLExplicit(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("DATABASE_READ_URL", "postgres://localhost/test-read")
	t.Setenv("CHAIN_ID", "324")

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DatabaseReadURL != "postgres://localhost/test-read" {
		t.Errorf("DatabaseReadURL = %q, want %q", cfg.DatabaseReadURL, "postgres://localhost/test-read")
	}
}

func TestLoad_ParsesRetryConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0xfactory")
	t.Setenv("RETRY_MAX_RETRIES", "10")
	t.Setenv("RETRY_BASE_DELAY", "1s")

	cfg, err := LoadIndexer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.RetryMaxRetries != 10 {
		t.Errorf("RetryMaxRetries = %d, want 10", cfg.RetryMaxRetries)
	}
	if cfg.RetryBaseDelay != 1*time.Second {
		t.Errorf("RetryBaseDelay = %v, want %v", cfg.RetryBaseDelay, 1*time.Second)
	}
}

func TestLoad_ParsesNumericFields(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CHAIN_ID", "42161")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("FACTORY_ADDRESS", "0xfactory")
	t.Setenv("START_BLOCK", "1000")
	t.Setenv("BLOCK_BATCH_SIZE", "500")
	t.Setenv("CONFIRMATIONS", "12")

	cfg, err := LoadIndexer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ChainID != 42161 {
		t.Errorf("ChainID = %d, want 42161", cfg.ChainID)
	}
	if cfg.StartBlock != 1000 {
		t.Errorf("StartBlock = %d, want 1000", cfg.StartBlock)
	}
	if cfg.BlockBatchSize != 500 {
		t.Errorf("BlockBatchSize = %d, want 500", cfg.BlockBatchSize)
	}
	if cfg.Confirmations != 12 {
		t.Errorf("Confirmations = %d, want 12", cfg.Confirmations)
	}
}
