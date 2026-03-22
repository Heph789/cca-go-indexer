package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Auction represents a decoded AuctionCreated event.
type Auction struct {
	AuctionAddress common.Address
	TokenOut       common.Address
	CurrencyIn     common.Address
	Owner          common.Address
	StartTime      uint64
	EndTime        uint64

	ChainID     int64
	BlockNumber uint64
	TxHash      common.Hash
	LogIndex    uint
	CreatedAt   time.Time
}
