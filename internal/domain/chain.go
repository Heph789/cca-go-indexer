// Package domain defines shared types used across the indexer.
package domain

// ChainID identifies an EVM chain. It is a type alias (not a distinct type)
// so it can be used interchangeably with int64 without conversion.
type ChainID = int64

// Well-known EVM chain IDs that the indexer may target.
const (
	ChainMainnet  ChainID = 1
	ChainBase     ChainID = 8453
	ChainUnichain ChainID = 130
	ChainSepolia  ChainID = 11155111
)
