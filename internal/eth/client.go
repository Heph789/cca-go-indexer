package eth

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type Client interface {
	BlockNumber(ctx context.Context) (uint64, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	Close()
}

func NewClient(rpcURL string) (Client, error) {
	httpClient := newHTTPClientWithRetry(RetryConfig{
		MaxRetries: 5,
		BaseDelay:  500 * time.Millisecond,
	})
	c, err := rpc.DialOptions(context.Background(), rpcURL, rpc.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}
	return ethclient.NewClient(c), nil
}
