package postgres

import "github.com/ethereum/go-ethereum/common"

func addrToText(addr common.Address) string {
	return addr.Hex()
}

func hashToText(hash common.Hash) string {
	return hash.Hex()
}
