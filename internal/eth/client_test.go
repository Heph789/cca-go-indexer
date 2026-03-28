package eth

import (
	"testing"
	"time"
)

func TestNewClient_SatisfiesInterface(t *testing.T) {
	// Use a URL that will fail to connect but still creates a client.
	// go-ethereum's ethclient.Dial only fails on invalid URL scheme,
	// not on unreachable hosts (connection is lazy).
	c, err := NewClient("http://localhost:1", RetryConfig{MaxRetries: 5, BaseDelay: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("expected no error for valid URL scheme, got: %v", err)
	}
	defer c.Close()

	// Verify it satisfies the Client interface.
	var _ Client = c
}

func TestNewClient_ErrorOnInvalidURL(t *testing.T) {
	_, err := NewClient("not-a-valid-url", RetryConfig{MaxRetries: 5, BaseDelay: 500 * time.Millisecond})
	if err == nil {
		t.Fatal("expected error for invalid RPC URL")
	}
}
