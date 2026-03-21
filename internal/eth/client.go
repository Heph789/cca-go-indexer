// Package eth defines the interface the indexer needs from an Ethereum RPC client.
// In production this is satisfied directly by go-ethereum's ethclient.Client
// (wired with a retrying HTTP transport — see transport.go).
// In tests, use a mock implementation.
package eth

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// Client defines the RPC operations the indexer needs.
// go-ethereum's *ethclient.Client satisfies this interface directly.
type Client interface {
	// BlockNumber returns the current chain head block number.
	BlockNumber(ctx context.Context) (uint64, error)

	// HeaderByNumber returns the header for a specific block.
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)

	// FilterLogs fetches logs matching the given filter criteria.
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)

	// Close releases RPC connection resources.
	Close()
}

// NewClient dials the given RPC endpoint and returns a Client.
// Uses a custom http.Client with retryTransport (see transport.go)
// so that transient HTTP errors are retried transparently.
func NewClient(rpcURL string, retryCfg RetryConfig) (Client, error) {
	// TODO:
	// 1. Create HTTP client with retry transport: newHTTPClientWithRetry(retryCfg)
	// 2. Dial RPC with custom HTTP client: rpc.DialOptions(ctx, rpcURL, rpc.WithHTTPClient(httpClient))
	// 3. Return ethclient.NewClient(rpcClient) — satisfies Client interface directly
	panic("not implemented")
}
