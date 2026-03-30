package cca

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Bid represents a bid placed in a CCA auction, decoded from a BidSubmitted event.
type Bid struct {
	ID             uint64
	Owner          common.Address
	PriceQ96       *big.Int
	Amount         *big.Int
	AuctionAddress common.Address
	ChainID        int64
	BlockNumber    uint64
	BlockTime      time.Time
	TxHash         common.Hash
	LogIndex       uint
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
