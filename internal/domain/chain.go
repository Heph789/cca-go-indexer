// Package domain defines shared types used across the indexer.
package domain

// ChainID identifies an EVM chain.
type ChainID = int64

const (
	ChainMainnet  ChainID = 1
	ChainBase     ChainID = 8453
	ChainUnichain ChainID = 130
	ChainSepolia  ChainID = 11155111
)
