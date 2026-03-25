package eth_test

import (
	"github.com/cca/go-indexer/internal/eth"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Compile-time check: *ethclient.Client satisfies eth.Client.
var _ eth.Client = (*ethclient.Client)(nil)
