// Package cca defines domain types for CCA contract events.
// These are plain structs with no DB or JSON tags — the store
// layer handles mapping to/from database rows.
package cca

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Auction represents a decoded AuctionCreated event from the CCA factory.
// Direct event fields: Auction (indexed), Token (indexed), Amount, ConfigData.
// ConfigData is ABI-decoded into the AuctionParameters fields below.
type Auction struct {
	// Direct event fields
	AuctionAddress common.Address
	Token          common.Address
	Amount         *big.Int

	// Decoded from configData (AuctionParameters struct)
	Currency              common.Address
	TokensRecipient       common.Address
	FundsRecipient        common.Address
	StartBlock            uint64
	EndBlock              uint64
	ClaimBlock            uint64
	TickSpacing           *big.Int
	ValidationHook        common.Address
	FloorPrice            *big.Int
	RequiredCurrencyRaised *big.Int

	// Block context — where this event appeared on-chain.
	ChainID     int64
	BlockNumber uint64
	TxHash      common.Hash
	LogIndex    uint
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
