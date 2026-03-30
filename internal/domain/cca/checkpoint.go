package cca

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Checkpoint represents a clearing price checkpoint from a CheckpointUpdated event.
type Checkpoint struct {
	BlockNumber      uint64   // event param: auction's logical block
	ClearingPriceQ96 *big.Int
	CumulativeMps    uint32
	AuctionAddress   common.Address
	ChainID          int64
	TxBlockNumber    uint64    // log.BlockNumber: chain block where tx was mined
	TxBlockTime      time.Time
	TxHash           common.Hash
	LogIndex         uint
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
