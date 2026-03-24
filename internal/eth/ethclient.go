package eth

import "github.com/ethereum/go-ethereum/ethclient"

// Dial connects to an Ethereum JSON-RPC endpoint and returns a Client.
func Dial(rawurl string) (Client, error) {
	return ethclient.Dial(rawurl)
}
