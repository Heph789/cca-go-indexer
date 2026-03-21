// Package cca defines domain types for CCA contract events.
// These are plain structs with no DB or JSON tags — the store
// layer handles mapping to/from database rows.
package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Auction represents a decoded AuctionCreated event.
// Fields will be finalized once the exact ABI is obtained.
type Auction struct {
	AuctionAddress common.Address
	TokenOut       common.Address
	CurrencyIn     common.Address
	Owner          common.Address
	StartTime      uint64
	EndTime        uint64

	// Block context — where this event appeared on-chain.
	ChainID     int64
	BlockNumber uint64
	TxHash      common.Hash
	LogIndex    uint
	CreatedAt   time.Time
}
