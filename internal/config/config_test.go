package config_test

import (
	"testing"
	"time"

	"github.com/cca/go-indexer/internal/config"
)

// setRequiredEnv sets all required environment variables to valid values.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
	t.Setenv("RPC_URL", "https://rpc.example.com")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("FACTORY_ADDRESS", "0x1234567890abcdef1234567890abcdef12345678")
}

func TestLoad_AllFieldsFromEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/mydb")
	t.Setenv("RPC_URL", "https://rpc.zksync.example.com")
	t.Setenv("CHAIN_ID", "324")
	t.Setenv("FACTORY_ADDRESS", "0xaabbccdd")
	t.Setenv("START_BLOCK", "5000")
	t.Setenv("POLL_INTERVAL", "5s")
	t.Setenv("BLOCK_BATCH_SIZE", "250")
	t.Setenv("CONFIRMATIONS", "20")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/mydb" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@localhost:5432/mydb")
	}
	if cfg.RPCURL != "https://rpc.zksync.example.com" {
		t.Errorf("RPCURL = %q, want %q", cfg.RPCURL, "https://rpc.zksync.example.com")
	}
	if cfg.ChainID != 324 {
		t.Errorf("ChainID = %d, want %d", cfg.ChainID, 324)
	}
	if cfg.FactoryAddress != "0xaabbccdd" {
		t.Errorf("FactoryAddress = %q, want %q", cfg.FactoryAddress, "0xaabbccdd")
	}
	if cfg.StartBlock != 5000 {
		t.Errorf("StartBlock = %d, want %d", cfg.StartBlock, 5000)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 5*time.Second)
	}
	if cfg.BlockBatchSize != 250 {
		t.Errorf("BlockBatchSize = %d, want %d", cfg.BlockBatchSize, 250)
	}
	if cfg.Confirmations != 20 {
		t.Errorf("Confirmations = %d, want %d", cfg.Confirmations, 20)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoad_Defaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.StartBlock != 0 {
		t.Errorf("StartBlock = %d, want default %d", cfg.StartBlock, 0)
	}
	if cfg.PollInterval != 12*time.Second {
		t.Errorf("PollInterval = %v, want default %v", cfg.PollInterval, 12*time.Second)
	}
	if cfg.BlockBatchSize != 100 {
		t.Errorf("BlockBatchSize = %d, want default %d", cfg.BlockBatchSize, 100)
	}
	if cfg.Confirmations != 12 {
		t.Errorf("Confirmations = %d, want default %d", cfg.Confirmations, 12)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
}

func TestLoad_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		omitVar string
	}{
		{name: "missing DATABASE_URL", omitVar: "DATABASE_URL"},
		{name: "missing RPC_URL", omitVar: "RPC_URL"},
		{name: "missing CHAIN_ID", omitVar: "CHAIN_ID"},
		{name: "missing FACTORY_ADDRESS", omitVar: "FACTORY_ADDRESS"},
	}

	requiredVars := map[string]string{
		"DATABASE_URL":    "postgres://user:pass@localhost:5432/testdb",
		"RPC_URL":         "https://rpc.example.com",
		"CHAIN_ID":        "324",
		"FACTORY_ADDRESS": "0x1234567890abcdef1234567890abcdef12345678",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range requiredVars {
				if k != tt.omitVar {
					t.Setenv(k, v)
				}
			}
			// Ensure the omitted var is unset.
			t.Setenv(tt.omitVar, "")

			_, err := config.Load()
			if err == nil {
				t.Errorf("Load() with %s unset: expected error, got nil", tt.omitVar)
			}
		})
	}
}

func TestLoad_InvalidChainID(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CHAIN_ID", "not-a-number")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() with invalid CHAIN_ID: expected error, got nil")
	}
}
