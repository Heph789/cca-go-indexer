// Package eth defines the interface the indexer needs from an Ethereum RPC client.
//
// By depending on an interface rather than *ethclient.Client directly, the
// indexer can be unit-tested with in-memory fakes (see fakes_test.go).
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
	// BlockNumber returns the most recent block number (chain head).
	BlockNumber(ctx context.Context) (uint64, error)

	// HeaderByNumber returns the header for a specific block. Used to record
	// block hashes and parent hashes for reorg detection.
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)

	// FilterLogs returns logs matching the given filter query (eth_getLogs).
	// The indexer uses this to fetch contract events within a block range.
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)

	// Close tears down the underlying RPC connection.
	Close()
}
