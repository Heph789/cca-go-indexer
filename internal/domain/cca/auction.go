// Package cca defines domain types for CCA contract events.
package cca

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Auction represents a decoded AuctionCreated event.
type Auction struct {
	// From event indexed params
	AuctionAddress common.Address
	Token          common.Address

	// From event non-indexed params
	TotalSupply *big.Int // "amount" in event = total token supply

	// From decoded configData (AuctionParameters)
	Currency               common.Address
	TokensRecipient        common.Address
	FundsRecipient         common.Address
	StartBlock             uint64
	EndBlock               uint64
	ClaimBlock             uint64
	TickSpacingQ96         *big.Int
	ValidationHook         common.Address
	FloorPriceQ96          *big.Int
	RequiredCurrencyRaised *big.Int // raw uint128, not Q96
	AuctionStepsData       []byte

	// Metadata from log
	EmitterContract common.Address
	ChainID         int64
	BlockNumber     uint64
	TxHash          common.Hash
	LogIndex        uint
	CreatedAt       time.Time
}
